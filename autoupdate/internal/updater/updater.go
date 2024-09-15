package updater

import (
	"archive/zip"
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"gopkg.in/ini.v1"
)

const (
	VersionFile = "ver.ini"

	GithubReleaseURL   = "https://github.com/%s/%s/%s/%s"
	GithubVersionURL   = "https://raw.githubusercontent.com/%s/%s/main/" + VersionFile
	ExitCodeNoUpdate   = 0
	ExitCodeNewVersion = 1
	ExitCodeError      = -1
	RetryLimit         = 3
	ChunkSize          = 1024 * 1024 // 1MB

)

type Updater struct {
	Current        VersionInfo
	Owner          string
	Repo           string
	ExecutableName string
	debugMode      bool
}

type VersionInfo struct {
	Version        string
	Filename       string
	MD5            string
	FullPackageURL string
}

func NewUpdater(owner, repo string, debug bool) *Updater {

	exepath, _ := os.Executable()
	execName := filepath.Base(exepath)

	u := &Updater{
		Current:        VersionInfo{},
		Owner:          owner,
		Repo:           repo,
		ExecutableName: execName,
		debugMode:      debug,
	}
	// 设置VersionFile为当前目录下VersionFile的绝对路径
	var VersionFilePath string
	execDir, err := filepath.Abs(filepath.Dir(exepath))
	if err != nil {
		log.Printf("获取可执行文件目录失败: %v\n", err)
		VersionFilePath = VersionFile
	} else {
		VersionFilePath = filepath.Join(execDir, VersionFile)
	}

	vi, err := ReadVersionFile(VersionFilePath)
	if err != nil {
		log.Printf("读取版本文件失败: %v\n", err)
	} else {
		u.Current = vi
		log.Printf("当前版本: %s\n", vi.Version)
	}

	return u
}

func (u *Updater) Update() int {

	vi, content, err := u.checkLatestVersion()
	if err != nil {
		log.Printf("检查更新时发生错误: %v", err)
		return ExitCodeError
	}

	if vi.Version == u.Current.Version {
		log.Printf("没有新版本")
		return ExitCodeNoUpdate
	}

	progressChan := make(chan float64)
	doneChan := make(chan bool)
	success := false

	// 更新进度条
	cb1 := func() {
		go func() {
			for progress := range progressChan {
				SetUpdateProgress(progress)
			}
		}()
	}

	// 更新完成
	cb2 := func() {
		go func() {
			success = <-doneChan
			if !success {
				ShowUpdateErrorDialog(err.Error())
				os.Exit(ExitCodeError)
			}

			// 关闭更新进度窗口
			CloseWindow()

		}()
	}

	// 在后台执行下载和更新
	cb3 := func() {
		go func() {
			err = u.downloadAndUpdate(vi, content, progressChan)
			if err != nil {
				ShowUpdateErrorDialog(err.Error())
				os.Exit(ExitCodeError)
			} else {
				close(progressChan)
				doneChan <- (err == nil)

			}

		}()
	}

	message := fmt.Sprintf("发现新版本: %s\n当前版本: %s\n是否更新?", vi.Version, u.Current.Version)
	if ShowUpdateConfirmDialog(message) {

		// 显示更新窗口
		ShowWindow(progressChan, cb1, cb2, cb3)

	} else {
		return ExitCodeNoUpdate
	}

	if !success {
		if err != nil {
			ShowUpdateErrorDialog(err.Error())
		}
		return ExitCodeError
	}

	return ExitCodeNewVersion

}

func (u *Updater) checkLatestVersion() (VersionInfo, []byte, error) {
	log.Printf("检查最新版本...")

	var vi VersionInfo
	var content []byte
	var err error

	for i := 0; i < 3; i++ {
		client := u.getHTTPClient()

		var resp *http.Response

		url := fmt.Sprintf(GithubVersionURL, u.Owner, u.Repo)
		resp, err = client.Get(url)
		if err != nil {
			time.Sleep(time.Second * 2)
			continue
		}
		defer resp.Body.Close()

		content, err = io.ReadAll(resp.Body)
		if err != nil {
			time.Sleep(time.Second * 2)
			continue
		}

		cfg, err := ini.Load(content)
		if err != nil {
			return vi, content, fmt.Errorf("无法解析版本信息: %v", err)
		}

		vi.Version = cfg.Section("").Key("version").String()
		vi.Filename = cfg.Section("").Key("filename").String()
		vi.MD5 = cfg.Section("").Key("md5").String()
		vi.FullPackageURL = cfg.Section("").Key("fullpackage").String()

		if vi.Version == "" || vi.Filename == "" || vi.MD5 == "" || vi.FullPackageURL == "" {
			return vi, content, fmt.Errorf("无效的版本文件格式")
		}

		return vi, content, nil
	}

	return vi, content, fmt.Errorf("检查更新失败 %v", err)
}

func (u *Updater) downloadAndUpdate(vi VersionInfo, versionFileContent []byte, progressChan chan<- float64) error {
	// 构建下载 URL
	url := fmt.Sprintf(GithubReleaseURL, u.Owner, u.Repo, vi.Version, vi.Filename)

	// 在当前目录下创建 tmp 目录
	tempDir := "tmp"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("创建临时目录失败: %v", err)
	}
	tempFilePath := filepath.Join(tempDir, vi.Filename)

	// 下载文件
	err := u.downloadWithResume(url, tempFilePath, progressChan)
	if err != nil {
		return fmt.Errorf("下载更新文件失败: %v", err)
	}

	// 验证 MD5
	downloadedMD5, err := calculateMD5(tempFilePath)
	if err != nil {
		return fmt.Errorf("计算下载文件 MD5 失败: %v", err)
	}
	if downloadedMD5 != vi.MD5 {
		os.Remove(tempFilePath)
		return fmt.Errorf("MD5 校验失败")
	}

	err = u.extractAndReplace(tempFilePath)
	if err != nil {
		return fmt.Errorf("更新失败: %v", err)
	}

	progressChan <- 1.0 // 100% 进度

	err = ioutil.WriteFile(VersionFile, versionFileContent, 0644)

	if err != nil {
		return fmt.Errorf("更新版本文件失败: %v", err)
	}

	return nil
}

func (u *Updater) downloadWithResume(url string, filePath string, progressChan chan<- float64) error {
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return err
	}
	downloadedSize := fileInfo.Size()
	totalSize := int64(0)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	if downloadedSize > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", downloadedSize))
	}

	client := u.getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// 服务器不支持断点续传，清空文件并重新下载
		if err := file.Truncate(0); err != nil {
			return fmt.Errorf("清空文件失败: %v", err)
		}
		if _, err := file.Seek(0, 0); err != nil {
			return fmt.Errorf("重置文件指针失败: %v", err)
		}
		totalSize = resp.ContentLength
		downloadedSize = 0
	case http.StatusPartialContent:
		totalSize = resp.ContentLength + downloadedSize
		if _, err := file.Seek(downloadedSize, 0); err != nil {
			return fmt.Errorf("设置文件指针失败: %v", err)
		}

		if totalSize == downloadedSize {
			progressChan <- 0.9
			return nil
		}

	default:
		return fmt.Errorf("服务器返回非预期状态码: %d", resp.StatusCode)
	}

	if totalSize <= 0 {
		return fmt.Errorf("无法获取文件大小")
	}

	var buffer []byte
	if u.debugMode {
		buffer = make([]byte, 1)
	} else {
		buffer = make([]byte, 32*1024)
	}
	for {
		if IsUpdateCancelled() {
			return fmt.Errorf("下载被用户取消")
		}
		if u.debugMode {
			time.Sleep(100 * time.Millisecond)
		}
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			_, writeErr := file.Write(buffer[:n])
			if writeErr != nil {
				return writeErr
			}
			downloadedSize += int64(n)
			progress := float64(downloadedSize) / float64(totalSize)
			if progress > 0.9 {
				progress = 0.9
			} else if progress < 0 {
				progress = 0
			}
			progressChan <- progress
		}

		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}

	return nil
}

func (u *Updater) downloadWithResume2(url string, filePath string, progressChan chan<- float64) error {
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return err
	}
	downloadedSize := fileInfo.Size()
	totalSize := int64(0)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", downloadedSize))

	client := u.getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		// 服务器不支持断点续传，清空文件并重新下载
		if err := file.Truncate(0); err != nil {
			return fmt.Errorf("清空文件失败: %v", err)
		}
		if _, err := file.Seek(0, 0); err != nil {
			return fmt.Errorf("重置文件指针失败: %v", err)
		}
		totalSize = resp.ContentLength
		downloadedSize = 0
	} else if resp.StatusCode == http.StatusPartialContent {
		totalSize = resp.ContentLength + downloadedSize
		if totalSize == downloadedSize {
			progressChan <- 0.9
			return nil
		}
	} else {
		return fmt.Errorf("服务器返回非预期状态码: %d", resp.StatusCode)
	}

	if totalSize <= 0 {
		return fmt.Errorf("无法获取文件大小")
	}

	var buffer []byte
	if u.debugMode {
		buffer = make([]byte, 1)
	} else {
		buffer = make([]byte, 32*1024)
	}
	for {
		if IsUpdateCancelled() {
			return fmt.Errorf("下载被用户取消")
		}
		if u.debugMode {
			time.Sleep(100 * time.Millisecond)
		}
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			_, writeErr := file.Write(buffer[:n])
			if writeErr != nil {
				return writeErr
			}
			downloadedSize += int64(n)
			progress := float64(downloadedSize) / float64(totalSize)
			if progress > 0.9 {
				progress = 0.9
			} else if progress < 0 {
				progress = 0
			}
			progressChan <- progress
		}

		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}

	return nil
}

func (u *Updater) extractAndReplace(zipPath string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("打开 ZIP 文件失败: %v", err)
	}
	defer reader.Close()

	// 获取当前可执行文件的目录
	execDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %v", err)
	}

	for _, file := range reader.File {
		filePath := filepath.Join(execDir, file.Name)

		if file.FileInfo().IsDir() {
			os.MkdirAll(filePath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
			return fmt.Errorf("创建目录失败: %v", err)
		}

		dstFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			return fmt.Errorf("创建文件失败: %v", err)
		}

		srcFile, err := file.Open()
		if err != nil {
			dstFile.Close()
			return fmt.Errorf("打开 ZIP 文件内容失败: %v", err)
		}

		_, err = io.Copy(dstFile, srcFile)
		srcFile.Close()
		dstFile.Close()

		if err != nil {
			return fmt.Errorf("复制文件内容失败: %v", err)
		}
	}

	return nil
}

func (u *Updater) replaceExecutable(tempPath, execPath string) error {
	switch runtime.GOOS {
	case "windows":
		return u.replaceWindowsExecutable(tempPath, execPath)
	case "darwin", "linux":
		return u.replaceUnixExecutable(tempPath, execPath)
	default:
		return fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}
}

func (u *Updater) replaceWindowsExecutable(tempPath, execPath string) error {
	// 在 Windows 上，我们需要使用 cmd 来替换文件
	cmd := exec.Command("cmd", "/C", "move", "/Y", tempPath, execPath)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd.Run()
}

func (u *Updater) replaceUnixExecutable(tempPath, execPath string) error {
	// 在 Unix 系统上，我们可以直接重命名文件
	return os.Rename(tempPath, execPath)
}

func (u *Updater) handleManualUpdate(versionInfo *VersionInfo) int {
	manualUpdate := ShowUpdateConfirmDialog("是否打开浏览器下载完整安装包?")
	if manualUpdate {
		u.openBrowser(versionInfo.FullPackageURL)
		return ExitCodeNewVersion
	}

	return ExitCodeError
}

func (u *Updater) openBrowser(url string) {
	var err error

	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}

	if err != nil {
		ShowUpdateErrorDialog(fmt.Sprintf("无法打开浏览器: %v", err))
	}
}

func calculateMD5(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func ReadVersionFile(filePath string) (vi VersionInfo, err error) {
	var content []byte
	content, err = ioutil.ReadFile(filePath)
	if err != nil {
		return vi, fmt.Errorf("无法读取版本文件: %v", err)
	}

	cfg, err := ini.Load(content)
	if err != nil {
		return vi, fmt.Errorf("无法解析版本信息: %v", err)
	}

	vi.Version = cfg.Section("").Key("version").String()
	vi.Filename = cfg.Section("").Key("filename").String()
	vi.MD5 = cfg.Section("").Key("md5").String()
	vi.FullPackageURL = cfg.Section("").Key("fullpackage").String()

	if vi.Version == "" || vi.Filename == "" || vi.MD5 == "" || vi.FullPackageURL == "" {
		return vi, fmt.Errorf("配置文件缺少必要的信息")
	}

	version := cfg.Section("").Key("version").String()
	if version == "" {
		return vi, fmt.Errorf("版本信息不存在")
	}

	return
}

func (u *Updater) getHTTPClient() *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	if u.debugMode {
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "tcp", "127.0.0.1:9808")
		}
		transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "tcp", "127.0.0.1:9808")
		}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   time.Second * 30,
	}
}
