package main

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/user"
	"path"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

type hopConfig struct {
	host      string
	port      int
	sshConfig *ssh.ClientConfig
}

type hop struct {
	config *hopConfig
	client *ssh.Client
}

func IsTerminal() bool {
	return terminal.IsTerminal(int(os.Stdin.Fd()))
}

func main() {
	if err := _main(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func _main() error {
	usr, err := user.Current()
	if err != nil {
		return err
	}
	defaultPrivateKeyPath := path.Join(usr.HomeDir, ".ssh", "id_rsa")
	reqGlobalPrivateKeyPath := defaultPrivateKeyPath

	// parse through arguments; the first that we don't recognise is the command to run
	// heuristic to try and detect hostname stuff by whether it has @ in it. Override with --cmd.
	hostPorts := []string{}
	commandToRun := []string{}
	var nextArgFn func(string) error = nil
	args := os.Args[1:]
	verbosity := 0
	for argI, arg := range args {
		naf := nextArgFn
		nextArgFn = nil
		if naf != nil {
			if err := naf(arg); err != nil {
				return err
			}
			continue
		}

		argIsShortOpt := false
		argIsLongOpt := false
		atCount := 0
		rest := arg

		for i, c := range arg {
			if i == 0 && c == '-' {
				argIsShortOpt = true
				continue
			} else if argIsShortOpt && i == 1 && c == '-' {
				argIsShortOpt = false
				argIsLongOpt = true
				continue
			}

			if argIsShortOpt {
				if c == 'v' {
					verbosity++
				} else if c == 'i' {
					// next arg is path to the key to use.
					rest = arg[i+1:] // TODO FIXME: parse the hop number
					nextArgFn = func(s string) error {
						reqGlobalPrivateKeyPath = s
						return nil
					}
					break
				} else {
					return fmt.Errorf("Unknown short option '%v'", c)
				}
			}
			if argIsLongOpt {
				rest = arg[i+1:]
				break
			}

			if c == '@' {
				atCount++
			}
		}

		if argIsShortOpt {
		} else if argIsLongOpt {
			if rest == "cmd" {
				commandToRun = args[argI+1:]
				break
			} else {
				return fmt.Errorf("unknown long opt: %v", rest)
			}
		} else if atCount > 0 {
			// assume this is a user@host thing
			hostPorts = append(hostPorts, arg)
		} else {
			// don't know what this is, probably the command
			commandToRun = args[argI:]
			break
		}
	}

	var privKeySigner ssh.Signer = nil
	if reqGlobalPrivateKeyPath != "" {
		if pemData, err := ioutil.ReadFile(reqGlobalPrivateKeyPath); err != nil {
			return err
		} else if signer, err := ssh.ParsePrivateKey(pemData); err != nil {
			return err
		} else {
			privKeySigner = signer
		}
	}
	authMethods := []ssh.AuthMethod{}
	if privKeySigner != nil {
		authMethods = append(authMethods, ssh.PublicKeys(privKeySigner))
	}
	// TODO FIXME: if stdin is a tty, add password / keyboard interactive prompters

	// assemble all the ssh config structs
	hopConfigs := []*hopConfig{}
	for _, hostPort := range hostPorts {
		hc := &hopConfig{
			host: "localhost",
			port: 22,
			sshConfig: &ssh.ClientConfig{
				Config: ssh.Config{},
				User:   usr.Username,
				Auth:   authMethods,
			},
		}
		if splitUserHost := strings.SplitN(hostPort, "@", 2); len(splitUserHost) == 2 {
			hc.sshConfig.User = splitUserHost[0]
			hc.host = splitUserHost[1]
		} else if len(splitUserHost) == 1 {
			hc.host = splitUserHost[0]
		}

		if splitHostPort := strings.SplitN(hc.host, ":", 2); len(splitHostPort) == 2 {
			if n, err := strconv.Atoi(splitHostPort[1]); err != nil {
				return err
			} else {
				hc.host = splitHostPort[0]
				hc.port = n
			}
		}
		hopConfigs = append(hopConfigs, hc)
	}

	if verbosity > 0 {
		fmt.Fprintln(os.Stderr, "hostports", hostPorts)
		fmt.Fprintln(os.Stderr, "cmd", commandToRun)
	}

	// now lets dial and run
	hops := []*hop{}
	dialFunc := net.Dial
	for _, hc := range hopConfigs {
		hostAddr := fmt.Sprintf("%s:%d", hc.host, hc.port)
		tcpConn, err := dialFunc("tcp", hostAddr)
		if err != nil {
			return err
		}
		defer tcpConn.Close()

		sshConn, chans, reqs, err := ssh.NewClientConn(tcpConn, hostAddr, hc.sshConfig)
		if err != nil {
			return err
		}

		client := ssh.NewClient(sshConn, chans, reqs)
		defer client.Close()
		dialFunc = client.Dial

		hops = append(hops, &hop{config: hc, client: client})
	}

	// run command
	if len(hops) == 0 {
		return fmt.Errorf("no ssh hops")
	}

	lastClient := hops[len(hops)-1].client
	session, err := lastClient.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	session.Stdin = os.Stdin
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	if len(commandToRun) > 0 {
		cmdString := strings.Join(commandToRun, " ") // should probably quote these more but who cares
		return session.Run(cmdString)
	} else {
		return fmt.Errorf("shell not implemented yet")
	}
}
