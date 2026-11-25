package sshnode

import (
	sshservice "github.com/sailry/sailry-code/modules/code/ssh/service"
	native "github.com/sailry/sailry-harness/sdk/native/node"
)

var _ native.SSHServiceProvider = module{}

func (module) NewSSHService(options native.SSHServiceOptions) native.SSHService {
	return sshservice.New(options)
}
