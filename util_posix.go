package daemon

import (
	"fmt"
	"os"
)

//return
func GetExecPath(pid int) (string, error) {
	proc_exe_link := fmt.Sprintf("/proc/%d/exe", pid)
	link_target, err := os.Readlink(proc_exe_link)
	if err != nil {
		return "", err
	}
	return link_target, nil
}

func IsProcessRunning(pid int) bool {
	my_path, err := GetExecPath(os.Getpid())
	if err != nil {
		return false
	}
	exe_path, err := GetExecPath(pid)
	if err != nil {
		return false
	}
	if my_path == exe_path {
		return true
	}
	return false
}
