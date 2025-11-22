package factory

type Architecture int

const (
	ArchAMD64 Architecture = iota
)

func (a Architecture) String() string {
	switch a {
	case ArchAMD64:
		return "amd64"
	default:
		return "unknown"
	}
}
