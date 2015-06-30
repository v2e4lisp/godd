package main

import (
        "flag"
        "fmt"
        "os"
        "os/exec"
        "path/filepath"

        "github.com/v2e4lisp/subcmd"
)

const VERSION = "0.1.0"

var (
        // command options
        procfile string
        envfile  string
        wd       string
)

func abort(msg ...interface{}) {
        fmt.Println(msg...)
        os.Exit(1)
}

func newcmd(cmd string, env []string) *exec.Cmd {
        c := exec.Command("sh", "-c", cmd)
        c.Dir = wd
        c.Env = env
        return c
}

func getenv() []string {
        env, err := LoadEnv(envfile)
        if err != nil {
                fmt.Println("Unable to load dotenv file. ",
                        "Using the current environment.")
                env = os.Environ()
        }
        return env
}

func main() {
        flag.Usage = func() {
                fmt.Fprint(os.Stderr, `COMMANDS:
  godd check             # Validate Procfile
  godd start [PROCESS]   # Start all processes(or a specific PROCESS)
  godd run [COMMAND]     # Load the dot env file and run any command.
  godd version           # Show version

OPTIONS:
`)
                flag.PrintDefaults()
        }
        flag.StringVar(&procfile, "f", "Procfile", "specify Procfile")
        flag.StringVar(&envfile, "e", ".env", "specify dot env file")
        flag.StringVar(&wd, "d", ".", "specify working dir")
        flag.Parse()

        var err error
        procfile, err = filepath.Abs(procfile)
        if err != nil {
                abort("Procfile error:", err.Error())
        }
        envfile, err = filepath.Abs(envfile)
        if err != nil {
                abort("Envfile error:", err.Error())
        }
        wd, err = filepath.Abs(wd)
        if err != nil {
                abort("Working dir error:", err.Error())
        }

        switch subcmd.Name() {
        case "start":
                doStart()
        case "check":
                doCheck()
        case "run":
                doRun()
        case "version":
                fmt.Println("godd", VERSION)
        case "":
                flag.Usage()
                os.Exit(1)
        default:
                abort("Command not found:", subcmd.Name())
        }
}

func doStart() {
        proc := ""
        if flag.NArg() > 1 {
                flag.Usage()
                os.Exit(1)
        }
        if flag.NArg() == 1 {
                proc = flag.Arg(0)
        }

        procs, err := LoadProcs(procfile)
        if err != nil {
                abort("Procfile error:", err)
        }
        if len(procs) == 0 {
                abort("Procfile error: no process is detected in", procfile)
        }

        env := getenv()
        cmds := []*Command(nil)
        for name, p := range procs {
                if proc != "" && proc != name {
                        continue
                }
                cmd := &Command{
                        name: name,
                        c:    newcmd(p, env),
                        exit: make(chan struct{}),
                }
                cmds = append(cmds, cmd)
        }
        if len(cmds) == 0 && proc != "" {
                abort("Proc not found:", proc)
        }
        Run(cmds)
}

func doCheck() {
        if flag.NArg() > 0 {
                flag.Usage()
                os.Exit(1)
        }

        _, err := LoadProcs(procfile)
        if err != nil {
                abort("Procfile error:", err)
        }
        fmt.Println("OK")
}

func doRun() {
        if flag.NArg() != 1 {
                flag.Usage()
                os.Exit(1)
        }

        env := getenv()
        c := newcmd(flag.Arg(0), env)
        c.Stdout = os.Stdout
        c.Stderr = os.Stderr
        c.Run()
}
