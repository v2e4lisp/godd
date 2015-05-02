package main

import (
        "flag"
        "fmt"
        "os"
        "os/exec"
        "path/filepath"
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
        env, err := loadEnv(envfile)
        if err != nil {
                fmt.Println("Unable to load dotenv file. ",
                        "Using the current environment.")
                env = os.Environ()
        }
        return env
}

func main() {
        flag.Usage = func() {
                fmt.Print(`COMMANDS:
  godd check             # Validate Procfile
  godd start [PROCESS]   # Start all processes(or a specific PROCESS)
  godd run [COMMAND]     # Load the dot env file and run any command.
  godd version           # Show version

OPTIONS:
`)
                flag.PrintDefaults()
        }
        flag.StringVar(&procfile, "p", "Procfile", "specify Procfile")
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

        if flag.NArg() < 1 {
                flag.Usage()
                os.Exit(1)
        }

        switch flag.Arg(0) {
        case "start":
                doStart()
        case "check":
                doCheck()
        case "run":
                doRun()
        case "version":
                fmt.Println("godd", VERSION)
        default:
                abort("Command not found:", flag.Arg(0))
        }
}

func doStart() {
        proc := ""
        if flag.NArg() > 2 {
                flag.Usage()
                os.Exit(1)
        }
        if flag.NArg() == 2 {
                proc = flag.Arg(1)
        }

        procs, err := loadProcs(procfile)
        if err != nil {
                abort("Procfile error:", err)
        }
        if len(procs) == 0 {
                abort("Procfile error: no process is detected in", procfile)
        }

        env := getenv()
        cmds := []*Command(nil)
        maxlen := 0
        for name, p := range procs {
                if proc != "" && proc != name {
                        continue
                }

                if len(name) > maxlen {
                        maxlen = len(name)
                }
                cmd := &Command{
                        name: name,
                        c:    newcmd(p, env),
                }
                cmds = append(cmds, cmd)
        }

        if len(cmds) == 0 && proc != "" {
                abort("Proc not found:", proc)
        }

        logPrefixFmt = "%-" + fmt.Sprintf("%d", maxlen) + "s"
        run(cmds)
}

func doCheck() {
        if flag.NArg() > 1 {
                flag.Usage()
                os.Exit(1)
        }

        _, err := loadProcs(procfile)
        if err != nil {
                abort("Procfile error:", err)
        }
        fmt.Println("OK")
}

func doRun() {
        if flag.NArg() != 2 {
                flag.Usage()
                os.Exit(1)
        }

        env := getenv()
        c := newcmd(flag.Arg(1), env)
        c.Stdout = os.Stdout
        c.Stderr = os.Stderr
        c.Run()
}
