package main

import (
        "errors"
        "fmt"
        "io/ioutil"
        "regexp"
        "strings"
)

var (
        procNamePat = regexp.MustCompile("^[a-zA-z0-9_]+$")
        envNamePat  = regexp.MustCompile("^[a-zA-z0-9_]+$")
)

func LoadEnv(envfile string) ([]string, error) {
        env := []string(nil)
        text, err := ioutil.ReadFile(envfile)
        if err != nil {
                return nil, err
        }
        lines := strings.Split(string(text), "\n")

        for ln, l := range lines {
                if l == "" || l[0] == '#' {
                        continue
                }

                e := strings.SplitN(l, "=", 2)
                if len(e) != 2 || !envNamePat.Match([]byte(e[0])) {
                        msg := fmt.Sprintf("parsing error at %s#%d:\n\t%s",
                                envfile, ln+1, l)
                        return nil, errors.New(msg)
                }

                key := e[0]
                val := strings.TrimSpace(e[1])
                last := len(val) - 1
                if val[0] == '\'' && val[last] == '\'' {
                        val = val[1:last]
                } else if val[0] == '"' && val[last] == '"' {
                        val = val[1:last]
                        val = strings.Replace(val, "\\\"", "\"", -1)
                        val = strings.Replace(val, "\\n", "\n", -1)
                }

                env = append(env, key+"="+val)
        }

        return env, nil
}

func LoadProcs(procfile string) (map[string]string, error) {
        procs := make(map[string]string)
        text, err := ioutil.ReadFile(procfile)
        if err != nil {
                return nil, err
        }
        lines := strings.Split(string(text), "\n")

        for ln, l := range lines {
                if l == "" || l[0] == '#' {
                        continue
                }
                p := strings.SplitN(l, ":", 2)
                if len(p) != 2 || !procNamePat.Match([]byte(p[0])) {
                        msg := fmt.Sprintf("parsing error at %s#%d:\n\t%s",
                                procfile, ln+1, l)
                        return nil, errors.New(msg)
                }
                procs[p[0]] = strings.TrimSpace(p[1])
        }

        return procs, nil
}
