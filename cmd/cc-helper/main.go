// cc-helper is a codesigned helper process that runs VMs on behalf of
// libcc clients. This allows the C bindings library to work without
// requiring the calling application to be codesigned with hypervisor
// entitlements on macOS.
package main

import "github.com/tinyrange/cc/internal/helper"

func main() {
	helper.Main()
}
