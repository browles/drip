package bencode

type RawMessage []byte

func (r *RawMessage) MarshalBencoding() ([]byte, error) {
	return []byte(*r), nil
}

func (r *RawMessage) UnmarshalBencoding(data []byte) error {
	*r = RawMessage(data)
	return nil
}
