# Auto-Update Application

This is an application with auto-update functionality.

## Features

- Automatic update checking
- Download and installation of new versions
- Multi-platform support (including Windows/MacOS/Linux-GTK/Linux-Cli)
- Supports running in GUI/command-line/silent mode

## Usage

./updater -h

-app string
        Application name
  -debug
        Debug mode
  -silent
        Silent mode


## Development

clone & open with vscode

## Debug

* run debug_server.py setup a mock server in 127.0.0.1:9808
* run output target with -debug or -silent from command line

### Prerequisites

- Go 1.16+
- go-winres
- upx

### Building

see launch.json & tasks.json

## Contributing

Issues and pull requests are welcome.

## Error Handling

The application includes a custom error handling for update processes.

## License

GPL v3
