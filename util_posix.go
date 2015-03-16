package daemon

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"path"
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
    content, err := ioutil.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
    if err != nil {
        return false
    }
    scanner := bufio.NewScanner(strings.NewReader(string(content)))
    scanner.Split(bufio.ScanWords)
    scanner.Scan()
    scanner.Scan()
    if scanner.Err() != nil {
        return false
    }
    pgname := scanner.Text()
    pgname = pgname[1:len(pgname)-1]
	real_path, err := GetExecPath(os.Getpid())
 	if err != nil {
		return false
	}
    if pgname == path.Base(real_path) {
        return true
    }
    return false
}
