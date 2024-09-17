import subprocess
import os
import sys

def patch_winres(target_file):
    script_dir = os.path.dirname(os.path.abspath(__file__))
    os.chdir(script_dir)
    print(f"Current working directory changed to: {script_dir}")
        
    cmds = [
        "go-winres simply",
        "go-winres patch ../bin/update.unpack.exe",
        "del ..\\bin\\*.bak"
        ]
    
    run_commands(cmds)

def run_commands(commands):
    for cmd in commands:
        try:
            subprocess.run(cmd, shell=True, check=True)
            print(f"Command executed successfully: {cmd}")
        except subprocess.CalledProcessError as e:
            print(f"Error executing command: {cmd}")
            print(f"Error message: {e}")
            return False
    return True

if __name__ == "__main__":
    args = sys.argv
    patch_winres(args[1])
        
