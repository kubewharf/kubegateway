package request

type RequestReaderWriterWrapper interface {
	RequestSize() int
	ResponseSize() int
	Status() int
	AddedInfo() string
}
