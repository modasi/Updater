import os
import mimetypes
from http.server import HTTPServer, SimpleHTTPRequestHandler
import json
import re

class DebugHandler(SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory="test_files", **kwargs)
    def send_error(self, code, message=None, explain=None):
        self.send_response(code)
        self.send_header('Content-type', 'application/json')
        self.end_headers()
        error_response = {
            "error": {
                "code": code,
                "message": message or "未知错误",
                "explain": explain or ""
            }
        }
        self.wfile.write(json.dumps(error_response, ensure_ascii=False).encode('utf-8'))

    def do_GET(self):
        if self.path == '/':
            response = json.dumps({"message": "调试服务器正在运行"})
            self.send_response(200)
            self.send_header('Content-type', 'application/json')
            self.send_header('Content-Length', str(len(response)))
            self.end_headers()
            self.wfile.write(response.encode())
        else:
            filename = os.path.basename(self.path.lstrip('/'))
            path = os.path.join(self.directory, filename)
            
            if os.path.isfile(path):
                file_size = os.path.getsize(path)
                start_range = 0
                end_range = file_size - 1

                if 'Range' in self.headers:
                    range_match = re.match(r'bytes=(\d+)-(\d*)', self.headers['Range'])
                    if range_match:
                        start_range = int(range_match.group(1))
                        if range_match.group(2):
                            end_range = min(int(range_match.group(2)), file_size - 1)
                        # 如果 end_range 缺失，保持为 file_size - 1
                    else:
                        self.send_error(400, "无效的 Range 头")
                        return

                content_length = end_range - start_range + 1

                self.send_response(206 if 'Range' in self.headers else 200)
                self.send_header('Content-type', mimetypes.guess_type(path)[0] or 'application/octet-stream')
                self.send_header('Content-Length', str(content_length))
                self.send_header('Accept-Ranges', 'bytes')
                self.send_header('Content-Range', f'bytes {start_range}-{end_range}/{file_size}')
                self.end_headers()

                with open(path, 'rb') as f:
                    f.seek(start_range)
                    self.wfile.write(f.read(content_length))
            else:
                self.send_error(404, "文件未找到")

def run_server(port=9808):
    current_dir = os.path.dirname(os.path.abspath(__file__))
    test_files_dir = os.path.join(current_dir, "test_files")
    
    server_address = ('', port)
    httpd = HTTPServer(server_address, DebugHandler)
    print(f"调试服务器正在运行，端口：{port}")
    print(f"根目录设置为：{test_files_dir}")
    httpd.serve_forever()

if __name__ == "__main__":
    run_server()