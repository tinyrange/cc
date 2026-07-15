//go:build windows

package ccvmd

func validateWorkerControlDirectory(string) error { return nil }
func secureWorkerControlSocket(string) error      { return nil }
