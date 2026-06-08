package whp

func execRequestKind(kind string) string {
	if kind == "" {
		return "exec"
	}
	return kind
}
