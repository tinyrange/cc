package guestinit

import _ "embed"

//go:embed guest-init-linux-arm64
var guestInitLinuxARM64 []byte

//go:embed guest-init-linux-amd64
var guestInitLinuxAMD64 []byte
