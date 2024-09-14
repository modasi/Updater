package updater

import (
	"archive/zip"
	"bufio"
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
	"strings"
	"time"

	"gopkg.in/ini.v1"
)

const (
	VersionFile        = "ver.ini"
	GithubReleaseURL   = "https://github.com/%s/%s/%s/%s"
	GithubVersionURL   = "https://raw.githubusercontent.com/%s/%s/main/ver.ini"
	ExitCodeNoUpdate   = 0
	ExitCodeNewVersion = 1
	ExitCodeError      = -1
	RetryLimit         = 3
	ChunkSize          = 1024 * 1024 // 1MB

)

type Updater struct {
	CurrentVersion string
	Owner          string
	Repo           string
	ExecutableName string
	IgnoredVersion string
	debugMode      bool
}

type VersionInfo struct {
	Version        string
	Filename       string
	MD5            string
	FullPackageURL string
}

func NewUpdater(owner, repo string, debug bool) *Updater {
	execName := filepath.Base(os.Args[0])

	u := &Updater{
		CurrentVersion: "1.0.0",
		Owner:          owner,
		Repo:           repo,
		ExecutableName: execName,
		debugMode:      debug,
	}

	currentVersion, err := ReadVersionFile(VersionFile)
	if err != nil {
		log.Printf("读取版本文件失败: %v\n", err)
	} else {
		u.CurrentVersion = currentVersion
		fmt.Printf("当前版本: %s\n", currentVersion)
	}

	return u
}

func (u *Updater) Update() int {

	versionInfo, err := u.checkLatestVersion()
	if err != nil {
		log.Printf("检查更新时发生错误: %v", err)
		return ExitCodeError
	}

	if versionInfo.Version == u.CurrentVersion || versionInfo.Version == u.IgnoredVersion {
		return ExitCodeNoUpdate
	}

	message := fmt.Sprintf("发现新版本: %s\n当前版本: %s\n是否更新?", versionInfo.Version, u.CurrentVersion)
	if ShowUpdateConfirmDialog(message) {
		progressChan := make(chan float64)
		doneChan := make(chan bool)

		// 在后台执行下载和更新
		go func() {
			err = u.downloadAndUpdate(versionInfo, progressChan)
			if err != nil {
				ShowUpdateErrorDialog(err.Error())
				os.Exit(ExitCodeError)
			}
			close(progressChan)
			doneChan <- (err == nil)

		}()

		// 更新进度条
		go func() {
			for progress := range progressChan {
				SetUpdateProgress(progress)
			}
		}()

		// 显示更新进度窗口
		ShowWindow(progressChan)

		// 等待下载和更新完成
		success := <-doneChan

		// 关闭更新进度窗口
		CloseWindow()

		if !success {
			ShowUpdateErrorDialog(err.Error())
			return ExitCodeError
		}

		return ExitCodeNewVersion
	} else {
		return ExitCodeNoUpdate
	}
}

func (u *Updater) checkLatestVersion() (*VersionInfo, error) {
	log.Printf("检查最新版本")
	var versionInfo VersionInfo
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

		scanner := bufio.NewScanner(resp.Body)

		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
			switch key {
			case "version":
				versionInfo.Version = value
			case "filename":
				versionInfo.Filename = value
			case "md5":
				versionInfo.MD5 = value
			case "fullpackage":
				versionInfo.FullPackageURL = value
			}
		}

		if versionInfo.Version == "" || versionInfo.Filename == "" || versionInfo.MD5 == "" || versionInfo.FullPackageURL == "" {
			return nil, fmt.Errorf("无效的版本文件格式")
		}
		return &versionInfo, nil
	}

	return nil, fmt.Errorf("检查更新失败 %v", err)
}

func (u *Updater) downloadAndUpdate(versionInfo *VersionInfo, progressChan chan<- float64) error {
	// 构建下载 URL
	url := fmt.Sprintf(GithubReleaseURL, u.Owner, u.Repo, versionInfo.Version, versionInfo.Filename)

	// 在当前目录下创建 tmp 目录
	tempDir := "tmp"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("创建临时目录失败: %v", err)
	}
	tempFilePath := filepath.Join(tempDir, versionInfo.Filename)

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
	if downloadedMD5 != versionInfo.MD5 {
		os.Remove(tempFilePath)
		return fmt.Errorf("MD5 校验失败")
	}

	// 解压并替换可执行文件
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前可执行文件路径失败: %v", err)
	}

	err = u.extractAndReplace(tempFilePath, execPath)
	if err != nil {
		return fmt.Errorf("更新失败: %v", err)
	}

	progressChan <- 1.0 // 100% 进度

	// 更新版本文件
	err = ioutil.WriteFile(VersionFile, []byte(versionInfo.Version), 0644)
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
			if progress > 1.0 {
				progress = 1.0
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

func (u *Updater) extractAndReplace(zipPath, execPath string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	targetName := fmt.Sprintf("%s_%s_%s", u.ExecutableName, runtime.GOOS, runtime.GOARCH)

	for _, file := range reader.File {
		if file.Name == targetName {
			rc, err := file.Open()
			if err != nil {
				return err
			}
			defer rc.Close()

			tempExec, err := ioutil.TempFile("", "exec_*")
			if err != nil {
				return err
			}
			defer os.Remove(tempExec.Name())

			_, err = io.Copy(tempExec, rc)
			if err != nil {
				return err
			}
			tempExec.Close()

			err = os.Chmod(tempExec.Name(), 0755)
			if err != nil {
				return err
			}

			return u.replaceExecutable(tempExec.Name(), execPath)
		}
	}

	return fmt.Errorf("未找到适合当前系统的更新文件")
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

	ignore := ShowUpdateConfirmDialog("是否忽略此版本?")
	if ignore {
		u.IgnoredVersion = versionInfo.Version
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

func ReadVersionFile(filePath string) (string, error) {
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("无法读取版本文件: %v", err)
	}

	// 将内容转换为INI格式
	iniContent := fmt.Sprintf("[version]\n%s", string(content))
	cfg, err := ini.Load([]byte(iniContent))
	if err != nil {
		return "", fmt.Errorf("无法解析版本信息: %v", err)
	}

	version := cfg.Section("version").Key("ver").String()
	if version == "" {
		return "", fmt.Errorf("版本信息不存在")
	}

	return strings.TrimSpace(version), nil
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
