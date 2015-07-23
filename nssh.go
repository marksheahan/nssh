package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path"
	"strconv"
	"strings"
	"text/template"

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
	indexedPrivateKeyPath := map[int]string{}

	// parse through arguments; the first that we don't recognise is the command to run
	// heuristic to try and detect hostname stuff by whether it has @ in it. Override with --cmd.
	hostPorts := []string{}
	runLocalWithForward := ""
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
				rest = arg[i+1:]
				break
			}

			if argIsShortOpt {
				if c == 'v' {
					verbosity++
				} else if c == 'i' {
					// next arg is path to the key to use.
					thisRest := arg[i+1:] // TODO FIXME: parse the hop number
					nextArgFn = func(s string) error {
						// if it's a -i path/to/key.pem, assume global
						// if it is indexed: -i2 path/to/key.pem then that means hop 2 uses this key
						// 0 based counting.
						if thisRest == "" {
							reqGlobalPrivateKeyPath = s
						} else if n, err := strconv.Atoi(thisRest); err != nil {
							return err
						} else {
							indexedPrivateKeyPath[n] = s
						}
						return nil
					}
					break
				} else {
					return fmt.Errorf("Unknown short option '%v'", c)
				}
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
			} else if rest == "run_local_fwd" {
				nextArgFn = func(s string) error {
					runLocalWithForward = s
					return nil
				}
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

	// assemble all the ssh config structs
	hopConfigs := []*hopConfig{}
	for hopIndex, hostPort := range hostPorts {

		// find the private key to use for this hop. try indexed, fall back to global
		var privKeySigner ssh.Signer = nil
		privateKeyPath := reqGlobalPrivateKeyPath
		if kp, ok := indexedPrivateKeyPath[hopIndex]; ok {
			privateKeyPath = kp
		}
		if verbosity > 1 {
			fmt.Fprintf(os.Stderr, "hop %d: private key path: %v\n", hopIndex, privateKeyPath)
		}

		if privateKeyPath != "" {
			if pemData, err := ioutil.ReadFile(privateKeyPath); err != nil {
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

		hc := &hopConfig{
			host: "localhost",
			port: 22,
			sshConfig: &ssh.ClientConfig{
				Config: ssh.Config{},
				User:   usr.Username,
				Auth:   authMethods,
			},
		}

		// split user@host:port into user, host, port, store in config properly
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

	if verbosity > 2 {
		fmt.Fprintln(os.Stderr, "hostports", hostPorts)
		fmt.Fprintln(os.Stderr, "cmd", commandToRun)
	}

	// check that there is at least some sshing happening
	if len(hopConfigs) == 0 {
		return fmt.Errorf("no ssh hops")
	}

	// some other early exits
	if runLocalWithForward != "" {
		if len(commandToRun) > 0 {
			// ok
		} else {
			return fmt.Errorf("run_local_fwd requested without command")
		}
	} else {
		if len(commandToRun) > 0 {
			// ok
		} else {
			return fmt.Errorf("shell not implemented yet")
		}
	}

	// now lets dial and run
	hops := []*hop{}
	dialFunc := net.Dial
	for hopIndex, hc := range hopConfigs {
		hostAddr := fmt.Sprintf("%s:%d", hc.host, hc.port)

		if verbosity > 0 {
			fmt.Fprintf(os.Stderr, "hop %d: tcp connect to %v\n", hopIndex, hostAddr)
		}

		tcpConn, err := dialFunc("tcp", hostAddr)
		if err != nil {
			return err
		}
		defer tcpConn.Close()

		if verbosity > 0 {
			fmt.Fprintf(os.Stderr, "hop %d: ssh connect to %v\n", hopIndex, hostAddr)
		}

		sshConn, chans, reqs, err := ssh.NewClientConn(tcpConn, hostAddr, hc.sshConfig)
		if err != nil {
			return err
		}

		client := ssh.NewClient(sshConn, chans, reqs)
		defer client.Close()
		dialFunc = client.Dial

		hops = append(hops, &hop{config: hc, client: client})
	}

	lastClient := hops[len(hops)-1].client

	if runLocalWithForward != "" {
		// we're setting up a port forward then running a local command with that port forward
		if len(commandToRun) > 0 {
			// set up a local listener, connect each accept to the remote host
			localTcpListener, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				return err
			}

			// templated replace {{fwd}} with the localTcpListener address
			// so the user does:
			// nssh --run_local_fwd targetserver:443 user@jumphost curl -k -L https://{{fwd}}/something
			// and fwd will get replaced with localhost:41245 (41245 is arbitrary port)
			templatedCommand := []string{}
			templateSource := map[string]interface{}{}
			fnMap := template.FuncMap{
				"fwd": func() string { return localTcpListener.Addr().String() },
			}

			for _, c := range commandToRun {
				tmpl, err := template.New("script").Funcs(fnMap).Parse(c)
				if err != nil {
					return err
				}
				buf := &bytes.Buffer{}
				if err := tmpl.Execute(buf, templateSource); err != nil {
					return err
				}
				templatedCommand = append(templatedCommand, string(buf.Bytes()))
			}

			if verbosity > 2 {
				fmt.Fprintln(os.Stderr, "templatedCommand", templatedCommand)
			}

			// set up a server daemon to listen to the local listener and initiate port forwards for each connection
			go func() {
				for {
					// accept something from the port forward
					localSvrConn, err := localTcpListener.Accept()
					if err != nil {
						break
					}

					// dial to the target host
					forwardedTcpConn, err := lastClient.Dial("tcp", runLocalWithForward)
					if err != nil {
						fmt.Fprintf(os.Stderr, "forwarded dial to '%v' failed: %v", runLocalWithForward, err)
						localSvrConn.Close()
					} else {
						go func() {
							defer forwardedTcpConn.Close()
							defer localSvrConn.Close()
							doneChan := make(chan error, 2)
							go func() {
								_, err := io.Copy(forwardedTcpConn, localSvrConn)
								doneChan <- err
							}()
							go func() {
								_, err := io.Copy(localSvrConn, forwardedTcpConn)
								doneChan <- err
							}()
							<-doneChan
							<-doneChan
						}()
					}
				}
			}()

			cmd := exec.Command(templatedCommand[0], templatedCommand[1:]...)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		} else {
			return fmt.Errorf("run_local_fwd requested without command")
		}
	} else {
		// regular run a remote program
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
}
