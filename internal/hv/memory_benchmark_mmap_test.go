package hv

type benchmarkGuestMapping interface {
	Bytes() []byte
	Close() error
}
