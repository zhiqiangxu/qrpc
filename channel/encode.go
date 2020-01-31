package channel

import "encoding/json"

// Marshal for encode msg into bytes
func Marshal(msg interface{}) (bytes []byte, err error) {
	if m, ok := msg.(interface{ Marshal() ([]byte, error) }); ok {
		bytes, err = m.Marshal()
		return
	}

	bytes, err = json.Marshal(msg)
	return
}

// Unmarshal for decode msg out of bytes
func Unmarshal(bytes []byte, msg interface{}) (err error) {
	if m, ok := msg.(interface{ Unmarshal([]byte) error }); ok {
		err = m.Unmarshal(bytes)
		return
	}

	err = json.Unmarshal(bytes, msg)
	return
}
