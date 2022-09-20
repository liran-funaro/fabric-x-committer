package codec

import (
	"log"

	"google.golang.org/grpc/encoding"
)

func init() {
	encoding.RegisterCodec(protoCodec{})
}

type protoCodec struct {
}

func (p protoCodec) Marshal(v interface{}) ([]byte, error) {
	return v.([]byte), nil
}

func (p protoCodec) Unmarshal(data []byte, v interface{}) error {
	ba, ok := v.([]byte)
	if !ok {
		log.Fatalf("Invalid type of struct: %+v", v)
	}

	for index, b := range data {
		ba[index] = b
	}
	return nil
}

func (p protoCodec) Name() string {
	return "customCodec"
}
