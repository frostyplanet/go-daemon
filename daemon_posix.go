package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"
)

// Mark of daemon process - system environment variable _GO_DAEMON=1
const (
	MARK_NAME  = "_GO_DAEMON"
	MARK_VALUE = "1"
)

// Default file permissions for log and pid files.
const FILE_PERM = os.FileMode(0640)

// A Context describes daemon context.
type Context struct {
	// If PidFileName is non-empty, parent process will try to create and lock
	// pid file with given name. Child process writes process id to file.
	PidFileName string
	// Permissions for new pid file.
	PidFilePerm os.FileMode

	// If LogFileName is non-empty, parent process will create file with given name
	// and will link to fd 2 (stderr) for child process.
	LogFileName string
	// Permissions for new log file.
	LogFilePerm os.FileMode

	// If WorkDir is non-empty, the child changes into the directory before
	// creating the process.
	WorkDir string
	// If Chroot is non-empty, the child changes root directory
	Chroot string

	// If Env is non-nil, it gives the environment variables for the
	// daemon-process in the form returned by os.Environ.
	// If it is nil, the result of os.Environ will be used.
	Env []string
	// If Args is non-nil, it gives the command-line args for the
	// daemon-process. If it is nil, the result of os.Args will be used
	// (without program name).
	Args []string

	// Credential holds user and group identities to be assumed by a daemon-process.
	Credential *syscall.Credential
	// If Umask is non-zero, the daemon-process call Umask() func with given value.
	Umask int

	// Struct contains only serializable public fields (!!!)
	abspath  string
	pidFile  *LockFile
	logFile  *os.File
	nullFile *os.File

	rpipe, wpipe *os.File
}

// Reborn runs second copy of current process in the given context.
// function executes separate parts of code in child process and parent process
// and provides demonization of child process. It look similar as the
// fork-daemonization, but goroutine-safe.
// In success returns *os.Process in parent process and nil in child process.
// Otherwise returns error.
func (d *Context) Reborn() (child *os.Process, err error) {
	if !WasReborn() {
		child, err = d.parent()
	} else {
		err = d.child()
	}
	return
}

// Search search daemons process by given in context pid file name.
// If success returns pointer on daemons os.Process structure,
// else returns error. Returns nil if filename is empty.
func (d *Context) Search() (daemon *os.Process, err error) {
	if len(d.PidFileName) > 0 {
		var pid int
		if pid, err = ReadPidFile(d.PidFileName); err != nil {
			return
		}
		daemon, err = os.FindProcess(pid)
	}
	return
}

// WasReborn returns true in child process (daemon) and false in parent process.
func WasReborn() bool {
	return os.Getenv(MARK_NAME) == MARK_VALUE
}

func (d *Context) parent() (child *os.Process, err error) {
	if err = d.prepareEnv(); err != nil {
		panic(err)
	}

	defer d.closeFiles()
	if err = d.openFiles(); err != nil {
		panic(err)
	}

	attr := &os.ProcAttr{
		Dir:   d.WorkDir,
		Env:   d.Env,
		Files: d.files(),
		Sys: &syscall.SysProcAttr{
			Setsid:     true,
		},
	}
	if child, err = os.StartProcess(d.abspath, d.Args, attr); err != nil {
		if d.pidFile != nil {
			d.pidFile.Remove()
		}
		panic(err)
	}
	d.rpipe.Close()
	encoder := json.NewEncoder(d.wpipe)
	err = encoder.Encode(d)

	return
}

func (d *Context) openFiles() (err error) {
	if d.PidFilePerm == 0 {
		d.PidFilePerm = FILE_PERM
	}
	if d.LogFilePerm == 0 {
		d.LogFilePerm = FILE_PERM
	}

	if d.nullFile, err = os.Open(os.DevNull); err != nil {
		return
	}

	if len(d.PidFileName) > 0 {
		if d.pidFile, err = OpenLockFile(d.PidFileName, d.PidFilePerm); err != nil {
			return
		}
		if err = d.pidFile.Lock(); err != nil {
			return
		}
	}

	if len(d.LogFileName) > 0 {
		if d.logFile, err = os.OpenFile(d.LogFileName,
			os.O_WRONLY|os.O_CREATE|os.O_APPEND, d.LogFilePerm); err != nil {
			return
		}
	}

	d.rpipe, d.wpipe, err = os.Pipe()
	return
}

func (d *Context) closeFiles() (err error) {
	cl := func(file **os.File) {
		if *file != nil {
			(*file).Close()
			*file = nil
		}
	}
	cl(&d.rpipe)
	cl(&d.wpipe)
	cl(&d.logFile)
	cl(&d.nullFile)
	if d.pidFile != nil {
		d.pidFile.Close()
		d.pidFile = nil
	}
	return
}

func (d *Context) prepareEnv() (err error) {
	// get the correct exec path even if process executed through symlink
	if d.abspath, err = GetExecPath(os.Getpid()); err != nil {
		panic(err)
	}

	if len(d.Args) == 0 {
		d.Args = os.Args
	}

	mark := fmt.Sprintf("%s=%s", MARK_NAME, MARK_VALUE)
	if len(d.Env) == 0 {
		d.Env = os.Environ()
	}
	d.Env = append(d.Env, mark)

	return
}

func (d *Context) files() (f []*os.File) {
	log := d.nullFile
	if d.logFile != nil {
		log = d.logFile
	}

	f = []*os.File{
		d.rpipe,    // (0) stdin
		log,        // (1) stdout
		log,        // (2) stderr
		d.nullFile, // (3) dup on fd 0 after initialization
	}

	if d.pidFile != nil {
		f = append(f, d.pidFile.File) // (4) pid file
	}
	return
}

var initialized = false

func (d *Context) child() (err error) {
	if initialized {
		return os.ErrInvalid
	}
	initialized = true

	decoder := json.NewDecoder(os.Stdin)
	if err = decoder.Decode(d); err != nil {
		return
	}

	if err = syscall.Close(0); err != nil {
		return
	}
	if err = syscall.Dup2(3, 0); err != nil {
		return
	}

	if len(d.PidFileName) > 0 {
		d.pidFile = NewLockFile(os.NewFile(4, d.PidFileName))
		if err = d.pidFile.WritePid(); err != nil {
			return
		}
	}

	if d.Umask != 0 {
		syscall.Umask(int(d.Umask))
	}
	if len(d.Chroot) > 0 {
		err = syscall.Chroot(d.Chroot)
	}
	if d.Credential != nil {
		if d.Credential.Gid > 0 {
			if err = syscall.Setgid(int(d.Credential.Gid)); err != nil {
				return
			}
		}
		if d.Credential.Uid > 0 {
			if err = syscall.Setuid(int(d.Credential.Uid)); err != nil {
				return
			}
		}
	}

	return
}

// Release provides correct pid-file release in daemon.
func (d *Context) Release() (err error) {
	if !initialized {
		return
	}
	if d.pidFile != nil {
		err = d.pidFile.Remove()
	}
	return
}

func (d *Context) Status() {
	p, _ := d.Search()
	if p == nil {
		fmt.Println("stopped")
		os.Exit(1)
	} else if IsProcessRunning(p.Pid, d.PidFileName) {
		fmt.Println("running")
		os.Exit(0)
	} else {
		fmt.Println("crashed")
		os.Exit(1)
	}
}

func (d *Context) getRunningProcess() (*os.Process, error){
	p, err := d.Search()
	if err != nil {
		return nil, err
	} else if (p != nil && IsProcessRunning(p.Pid, d.PidFileName)) {
		return p, nil
	}
	return nil, err
}

func (d *Context) Stop() {
	p, _ := d.getRunningProcess()
	if p == nil {
		fmt.Println("not running")
		os.Exit(1)
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		panic(err)
	}
	p.Wait()
	fmt.Println("stopped")
	os.Remove(d.PidFileName)
	os.Exit(0)
}

func (d *Context) Kill() {
	p, _ := d.getRunningProcess()
	if p == nil {
		fmt.Println("not running")
		os.Exit(1)
	}
	if err := p.Kill(); err != nil {
		panic(err)
	}
	fmt.Println("killed")
	os.Remove(d.PidFileName)
	os.Exit(0)
}

func (d *Context) Start() {
	p, err := d.Search()
	if p != nil {
		if IsProcessRunning(p.Pid, d.PidFileName) {
			fmt.Println("daemon already running")
			os.Exit(1)
		}
	}
	p, err = d.Reborn()
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	if p != nil {
		fmt.Println("started")
		os.Exit(0)
	}
}

