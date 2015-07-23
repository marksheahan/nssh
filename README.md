# nssh
Golang command line ssh utility for running programs remotely over multiple hops.

Useful for executing things through a jump host when the jump host is a single entry point, or it is NATing etc.

## Why write this?

This: http://sshmenu.sourceforge.net/articles/transparent-mulithop.html is pretty cool. I wanted something similar, but totally self contained on the command line and easier to include in shell scripts etc.

My specific use case is automated provisioning for scale testing with temporary AWS instances, using a NATing jump host instead of allocating public IPs for all of the target nodes. 

## Example
With a network of:
workstation <----> server1 <----> server2 <----> desiredserver
We want to run 'ls -l' on desiredserver 

Assuming that authorized_keys is set up on server1, server2 and desiredserver to contain workstation's id_rsa.pub, one can run:

nssh user1@server1 user2@server2 user3@desiredserver ls -l

## TODO (not likely to happen soon though)
- Get 'shell' going, not just 'exec'. But I recommend still just using openssh if fully interactive anyway. This is a helpful test util for deployment automation when using jump hosts
- Support command line options for different keys at different hops, something like:
  - nssh -i1 ~/.ssh/jumphost.pem -i2 ~/.ssh/dev_boxes.pem mark@jumphost mark@dev_boxes ls -l
- If we're on a TTY, password / keyboardinteractive callback for auth on hops where we don't have keys installed

