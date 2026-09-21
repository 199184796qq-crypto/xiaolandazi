package douyin

import (
	"encoding/binary"
	"errors"
	"fmt"
)

var errInvalidProto = errors.New("invalid protobuf payload")

type wireField struct {
	number int
	wire   int
	value  uint64
	bytes  []byte
}

func parseFields(data []byte) ([]wireField, error) {
	fields := make([]wireField, 0, 12)

	for len(data) > 0 {
		tag, n := binary.Uvarint(data)
		if n <= 0 {
			return nil, errInvalidProto
		}
		data = data[n:]

		number := int(tag >> 3)
		wire := int(tag & 0x7)
		if number <= 0 {
			return nil, errInvalidProto
		}

		field := wireField{number: number, wire: wire}

		switch wire {
		case 0:
			value, n := binary.Uvarint(data)
			if n <= 0 {
				return nil, errInvalidProto
			}
			field.value = value
			data = data[n:]
		case 1:
			if len(data) < 8 {
				return nil, errInvalidProto
			}
			field.bytes = data[:8]
			data = data[8:]
		case 2:
			length, n := binary.Uvarint(data)
			if n <= 0 {
				return nil, errInvalidProto
			}
			data = data[n:]
			if length > uint64(len(data)) {
				return nil, errInvalidProto
			}
			field.bytes = data[:int(length)]
			data = data[int(length):]
		case 5:
			if len(data) < 4 {
				return nil, errInvalidProto
			}
			field.bytes = data[:4]
			data = data[4:]
		default:
			return nil, fmt.Errorf("%w: unsupported wire type %d", errInvalidProto, wire)
		}

		fields = append(fields, field)
	}

	return fields, nil
}

func fieldBytes(fields []wireField, number int) []byte {
	for _, field := range fields {
		if field.number == number && field.wire == 2 {
			return field.bytes
		}
	}
	return nil
}

func fieldString(fields []wireField, number int) string {
	return string(fieldBytes(fields, number))
}

func fieldVarint(fields []wireField, number int) uint64 {
	for _, field := range fields {
		if field.number == number && field.wire == 0 {
			return field.value
		}
	}
	return 0
}

func repeatedBytes(fields []wireField, number int) [][]byte {
	values := make([][]byte, 0, 4)
	for _, field := range fields {
		if field.number == number && field.wire == 2 {
			values = append(values, field.bytes)
		}
	}
	return values
}
