package guestinit

var embeddedPayloads = map[string][]byte{}

func embeddedPayload(arch string) []byte {
	return embeddedPayloads[arch]
}
