# nssh
Golang command line ssh utility for running programs remotely over multiple hops.

Useful for executing things on a remote node via a jump host.

## Why write this?

This: http://sshmenu.sourceforge.net/articles/transparent-mulithop.html is pretty cool. I wanted something similar, but totally self contained on the command line and easier to include in shell scripts etc.

My specific use case is automated provisioning for scale testing with temporary AWS instances, using a NATing jump host instead of allocating public IPs for all of the target nodes. 

## Example

### Running commands remotely
With this network, we want to run 'ls -l' on desiredserver:

_workstation <----> jumphost <----> desiredserver_

Assuming that authorized_keys is set up on server1, server2 and desiredserver to contain workstation's id_rsa.pub, one can run:

*nssh user1@jumphost user2@desiredserver ls -l*

The server hops are determined heuristically by the presence of '@' in the argument. If you're actually running a command with an '@' in it, an override is possible:

*nssh user1@jumphost user2@desiredserver --cmd stupidly_named@program*

Specifying different private keys for different hops:

*nssh -i0 ~/.ssh/jumphost.pem -i1 ~/.ssh/desiredserver.pem user1@jumphost user2@desiredserver ls -l*

### Setting up a jump + port forward, then running a command _locally_ that uses the port forward

This is a nice self-contained way of doing:
- Cmd 1: *ssh -L 12345:desired_server:443 user@jumphost*
- Cmd 2: *curl -k -L https://localhost:12345*
- Kill Cmd 1 (or forget about it...)

*nssh user@jumphost --run_local_fwd desired_server:443 curl -k -L https://{{fwd}}/api/dosomething*

What this does is:
- set up a local tcp listener on some random high port. This becomes {{fwd}} in the template.
- ssh authenticate to user@jumphost
- start executing the command (in this example, curl -k -L https://blah). The command is run through a templating thing that replaced {{fwd}} with the local listener.
- listen on the local tcp socket; for each connection coming in, forward to desired_server:443, stream socket data both ways
- once the command has finished, this process collapses, and therefore the ssh + all port forwards are cleaned up by the OS

This makes it easy to script up port forwarded curl requests through a jump host, without needing to manually manage multiple processes.

## TODO (not likely to happen soon though)
- Get 'shell' going, not just 'exec'. But I recommend still just using openssh if fully interactive anyway. This is a helpful test util for deployment automation when using jump hosts
- If we're on a TTY, password / keyboardinteractive callback for auth on hops where we don't have keys installed
- -L and -R options for various port forwards

