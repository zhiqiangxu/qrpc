package sugar

// Marshaler for Marshal/Unmarshal
type Marshaler interface {
	Marshal() ([]byte, error)
	Unmarshal([]byte) error
}

// Output for output
type Output interface {
	SetError(error)
}
