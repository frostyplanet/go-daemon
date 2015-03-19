package daemon

import (
	"os"
	"math"
	"strings"
	"fmt"
)

//return
func GetExecPath(pid int) (string, error) {
	proc_exe_link := fmt.Sprintf("/proc/%d/exe", pid)
	link_target, err := os.Readlink(proc_exe_link)
	if err != nil {
		return "", err
	}
	link_target = strings.TrimRight(link_target, " (deleted)") //if exe file is replace
	return link_target, nil
}

func IsProcessRunning(pid int, pidfiles... string) bool {
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
	if len(pidfiles) > 0 {
		//guessing if original pidfile is valid
		//assume pid file created not long after process start, pid number is not reuse
		pidfile := pidfiles[0]
		pidfile_s, err := os.Stat(pidfile)
		if err != nil { return false }
		proc_stat_path := fmt.Sprintf("/proc/%d/stat", pid)
		proc_s, err := os.Stat(proc_stat_path)
		if err != nil { return false }
		time_diff := pidfile_s.ModTime().Unix() - proc_s.ModTime().Unix()
		if math.Abs(float64(time_diff)) < 60.0 {
			return true
		}
	}
	return false
}
