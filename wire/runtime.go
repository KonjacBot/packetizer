package wire

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/big"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/KonjacBot/go-mc/chat"
	"github.com/KonjacBot/go-mc/nbt"
	"github.com/google/uuid"
)

type ReaderFrom interface {
	ReadFrom(io.Reader) (int64, error)
}

type WriterTo interface {
	WriteTo(io.Writer) (int64, error)
}

type LPVec3 struct {
	X float64
	Y float64
	Z float64
}

type BigInt = big.Int

const (
	lpVec3DataBitsMask = 32767
	lpVec3MaxQuantized = 32766.0
	lpVec3AbsMinValue  = 3.051944088384301e-5
	lpVec3AbsMaxValue  = 1.7179869183e10
	maxVarIntLen       = 5
	maxVarLongLen      = 10
)

func sanitizeLPVec3Value(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	if v < -lpVec3AbsMaxValue {
		return -lpVec3AbsMaxValue
	}
	if v > lpVec3AbsMaxValue {
		return lpVec3AbsMaxValue
	}
	return v
}

func packLPVec3Value(v float64) uint64 {
	return uint64(math.Round((v*0.5 + 0.5) * lpVec3MaxQuantized))
}

func unpackLPVec3Value(packed uint64, shift uint) float64 {
	val := (packed >> shift) & lpVec3DataBitsMask
	if val > 32766 {
		val = 32766
	}
	return (float64(val)*2.0)/lpVec3MaxQuantized - 1.0
}

func SizeBool(bool) (int, error) { return 1, nil }

func AppendBool(dst []byte, v bool) ([]byte, error) {
	if v {
		return append(dst, 1), nil
	}
	return append(dst, 0), nil
}

func DecodeBool(src []byte, out *bool) ([]byte, error) {
	if len(src) < 1 {
		return nil, io.ErrUnexpectedEOF
	}
	*out = src[0] != 0
	return src[1:], nil
}

func SizeInt8(int8) (int, error)       { return 1, nil }
func SizeUint8(uint8) (int, error)     { return 1, nil }
func SizeInt16(int16) (int, error)     { return 2, nil }
func SizeUint16(uint16) (int, error)   { return 2, nil }
func SizeInt32(int32) (int, error)     { return 4, nil }
func SizeUint32(uint32) (int, error)   { return 4, nil }
func SizeInt64(int64) (int, error)     { return 8, nil }
func SizeUint64(uint64) (int, error)   { return 8, nil }
func SizeFloat32(float32) (int, error) { return 4, nil }
func SizeFloat64(float64) (int, error) { return 8, nil }

func appendFixed(dst []byte, size int, write func([]byte)) []byte {
	start := len(dst)
	dst = append(dst, make([]byte, size)...)
	write(dst[start:])
	return dst
}

func AppendInt8(dst []byte, v int8) ([]byte, error)   { return append(dst, byte(v)), nil }
func AppendUint8(dst []byte, v uint8) ([]byte, error) { return append(dst, v), nil }
func AppendInt16(dst []byte, v int16) ([]byte, error) {
	return appendFixed(dst, 2, func(buf []byte) { binary.BigEndian.PutUint16(buf, uint16(v)) }), nil
}
func AppendInt16LE(dst []byte, v int16) ([]byte, error) {
	return appendFixed(dst, 2, func(buf []byte) { binary.LittleEndian.PutUint16(buf, uint16(v)) }), nil
}
func AppendUint16(dst []byte, v uint16) ([]byte, error) {
	return appendFixed(dst, 2, func(buf []byte) { binary.BigEndian.PutUint16(buf, v) }), nil
}
func AppendUint16LE(dst []byte, v uint16) ([]byte, error) {
	return appendFixed(dst, 2, func(buf []byte) { binary.LittleEndian.PutUint16(buf, v) }), nil
}
func AppendInt32(dst []byte, v int32) ([]byte, error) {
	return appendFixed(dst, 4, func(buf []byte) { binary.BigEndian.PutUint32(buf, uint32(v)) }), nil
}
func AppendInt32LE(dst []byte, v int32) ([]byte, error) {
	return appendFixed(dst, 4, func(buf []byte) { binary.LittleEndian.PutUint32(buf, uint32(v)) }), nil
}
func AppendUint32(dst []byte, v uint32) ([]byte, error) {
	return appendFixed(dst, 4, func(buf []byte) { binary.BigEndian.PutUint32(buf, v) }), nil
}
func AppendUint32LE(dst []byte, v uint32) ([]byte, error) {
	return appendFixed(dst, 4, func(buf []byte) { binary.LittleEndian.PutUint32(buf, v) }), nil
}
func AppendInt64(dst []byte, v int64) ([]byte, error) {
	return appendFixed(dst, 8, func(buf []byte) { binary.BigEndian.PutUint64(buf, uint64(v)) }), nil
}
func AppendInt64LE(dst []byte, v int64) ([]byte, error) {
	return appendFixed(dst, 8, func(buf []byte) { binary.LittleEndian.PutUint64(buf, uint64(v)) }), nil
}
func AppendUint64(dst []byte, v uint64) ([]byte, error) {
	return appendFixed(dst, 8, func(buf []byte) { binary.BigEndian.PutUint64(buf, v) }), nil
}
func AppendUint64LE(dst []byte, v uint64) ([]byte, error) {
	return appendFixed(dst, 8, func(buf []byte) { binary.LittleEndian.PutUint64(buf, v) }), nil
}
func AppendFloat32(dst []byte, v float32) ([]byte, error) {
	return AppendUint32(dst, math.Float32bits(v))
}
func AppendFloat32LE(dst []byte, v float32) ([]byte, error) {
	return AppendUint32LE(dst, math.Float32bits(v))
}
func AppendFloat64(dst []byte, v float64) ([]byte, error) {
	return AppendUint64(dst, math.Float64bits(v))
}
func AppendFloat64LE(dst []byte, v float64) ([]byte, error) {
	return AppendUint64LE(dst, math.Float64bits(v))
}

func decodeFixed[T any](src []byte, size int, read func([]byte) T, out *T) ([]byte, error) {
	if len(src) < size {
		return nil, io.ErrUnexpectedEOF
	}
	*out = read(src[:size])
	return src[size:], nil
}

func DecodeInt8(src []byte, out *int8) ([]byte, error) {
	if len(src) < 1 {
		return nil, io.ErrUnexpectedEOF
	}
	*out = int8(src[0])
	return src[1:], nil
}
func DecodeUint8(src []byte, out *uint8) ([]byte, error) {
	if len(src) < 1 {
		return nil, io.ErrUnexpectedEOF
	}
	*out = src[0]
	return src[1:], nil
}
func DecodeInt16(src []byte, out *int16) ([]byte, error) {
	return decodeFixed(src, 2, func(buf []byte) int16 { return int16(binary.BigEndian.Uint16(buf)) }, out)
}
func DecodeInt16LE(src []byte, out *int16) ([]byte, error) {
	return decodeFixed(src, 2, func(buf []byte) int16 { return int16(binary.LittleEndian.Uint16(buf)) }, out)
}
func DecodeUint16(src []byte, out *uint16) ([]byte, error) {
	return decodeFixed(src, 2, func(buf []byte) uint16 { return binary.BigEndian.Uint16(buf) }, out)
}
func DecodeUint16LE(src []byte, out *uint16) ([]byte, error) {
	return decodeFixed(src, 2, func(buf []byte) uint16 { return binary.LittleEndian.Uint16(buf) }, out)
}
func DecodeInt32(src []byte, out *int32) ([]byte, error) {
	return decodeFixed(src, 4, func(buf []byte) int32 { return int32(binary.BigEndian.Uint32(buf)) }, out)
}
func DecodeInt32LE(src []byte, out *int32) ([]byte, error) {
	return decodeFixed(src, 4, func(buf []byte) int32 { return int32(binary.LittleEndian.Uint32(buf)) }, out)
}
func DecodeUint32(src []byte, out *uint32) ([]byte, error) {
	return decodeFixed(src, 4, func(buf []byte) uint32 { return binary.BigEndian.Uint32(buf) }, out)
}
func DecodeUint32LE(src []byte, out *uint32) ([]byte, error) {
	return decodeFixed(src, 4, func(buf []byte) uint32 { return binary.LittleEndian.Uint32(buf) }, out)
}
func DecodeInt64(src []byte, out *int64) ([]byte, error) {
	return decodeFixed(src, 8, func(buf []byte) int64 { return int64(binary.BigEndian.Uint64(buf)) }, out)
}
func DecodeInt64LE(src []byte, out *int64) ([]byte, error) {
	return decodeFixed(src, 8, func(buf []byte) int64 { return int64(binary.LittleEndian.Uint64(buf)) }, out)
}
func DecodeUint64(src []byte, out *uint64) ([]byte, error) {
	return decodeFixed(src, 8, func(buf []byte) uint64 { return binary.BigEndian.Uint64(buf) }, out)
}
func DecodeUint64LE(src []byte, out *uint64) ([]byte, error) {
	return decodeFixed(src, 8, func(buf []byte) uint64 { return binary.LittleEndian.Uint64(buf) }, out)
}
func DecodeFloat32(src []byte, out *float32) ([]byte, error) {
	var bits uint32
	rest, err := DecodeUint32(src, &bits)
	if err != nil {
		return nil, err
	}
	*out = math.Float32frombits(bits)
	return rest, nil
}
func DecodeFloat32LE(src []byte, out *float32) ([]byte, error) {
	var bits uint32
	rest, err := DecodeUint32LE(src, &bits)
	if err != nil {
		return nil, err
	}
	*out = math.Float32frombits(bits)
	return rest, nil
}
func DecodeFloat64(src []byte, out *float64) ([]byte, error) {
	var bits uint64
	rest, err := DecodeUint64(src, &bits)
	if err != nil {
		return nil, err
	}
	*out = math.Float64frombits(bits)
	return rest, nil
}
func DecodeFloat64LE(src []byte, out *float64) ([]byte, error) {
	var bits uint64
	rest, err := DecodeUint64LE(src, &bits)
	if err != nil {
		return nil, err
	}
	*out = math.Float64frombits(bits)
	return rest, nil
}

func SizeVarInt(v int32) (int, error) {
	n := 1
	for uv := uint32(v); uv >= 0x80; uv >>= 7 {
		n++
	}
	return n, nil
}

func AppendVarInt(dst []byte, v int32) ([]byte, error) {
	uv := uint32(v)
	for {
		b := byte(uv & 0x7f)
		uv >>= 7
		if uv != 0 {
			dst = append(dst, b|0x80)
			continue
		}
		dst = append(dst, b)
		return dst, nil
	}
}

func DecodeVarInt(src []byte, out *int32) ([]byte, error) {
	var value uint32
	for i := 0; i < maxVarIntLen; i++ {
		if len(src) <= i {
			return nil, io.ErrUnexpectedEOF
		}
		b := src[i]
		value |= uint32(b&0x7F) << (uint(i) * 7)
		if b&0x80 == 0 {
			*out = int32(value)
			return src[i+1:], nil
		}
	}
	return nil, fmt.Errorf("varint too large")
}

func SizeVarLong(v int64) (int, error) {
	n := 1
	for uv := uint64(v); uv >= 0x80; uv >>= 7 {
		n++
	}
	return n, nil
}

func AppendVarLong(dst []byte, v int64) ([]byte, error) {
	uv := uint64(v)
	for {
		b := byte(uv & 0x7f)
		uv >>= 7
		if uv != 0 {
			dst = append(dst, b|0x80)
			continue
		}
		dst = append(dst, b)
		return dst, nil
	}
}

func DecodeVarLong(src []byte, out *int64) ([]byte, error) {
	var value uint64
	for i := 0; i < maxVarLongLen; i++ {
		if len(src) <= i {
			return nil, io.ErrUnexpectedEOF
		}
		b := src[i]
		value |= uint64(b&0x7F) << (uint(i) * 7)
		if b&0x80 == 0 {
			*out = int64(value)
			return src[i+1:], nil
		}
	}
	return nil, fmt.Errorf("varlong too large")
}

func SizeVarInt64(v int64) (int, error)                     { return SizeVarLong(v) }
func AppendVarInt64(dst []byte, v int64) ([]byte, error)    { return AppendVarLong(dst, v) }
func DecodeVarInt64(src []byte, out *int64) ([]byte, error) { return DecodeVarLong(src, out) }

func SizeZigzag32(v int32) (int, error) {
	return SizeVarInt(int32(uint32(v<<1) ^ uint32(v>>31)))
}

func AppendZigzag32(dst []byte, v int32) ([]byte, error) {
	return AppendVarInt(dst, int32(uint32(v<<1)^uint32(v>>31)))
}

func DecodeZigzag32(src []byte, out *int32) ([]byte, error) {
	var raw int32
	rest, err := DecodeVarInt(src, &raw)
	if err != nil {
		return nil, err
	}
	u := uint32(raw)
	*out = int32((u >> 1) ^ uint32(-int32(u&1)))
	return rest, nil
}

func SizeZigzag64(v int64) (int, error) {
	return SizeVarLong(int64(uint64(v<<1) ^ uint64(v>>63)))
}

func AppendZigzag64(dst []byte, v int64) ([]byte, error) {
	return AppendVarLong(dst, int64(uint64(v<<1)^uint64(v>>63)))
}

func DecodeZigzag64(src []byte, out *int64) ([]byte, error) {
	var raw int64
	rest, err := DecodeVarLong(src, &raw)
	if err != nil {
		return nil, err
	}
	u := uint64(raw)
	*out = int64((u >> 1) ^ uint64(-int64(u&1)))
	return rest, nil
}

func normalizeUnsigned128(v *BigInt) (*big.Int, error) {
	if v == nil {
		return big.NewInt(0), nil
	}
	out := new(big.Int).Set(v)
	if out.Sign() < 0 {
		mod := new(big.Int).Lsh(big.NewInt(1), 128)
		out.Add(out, mod)
	}
	if out.Sign() < 0 || out.BitLen() > 128 {
		return nil, fmt.Errorf("value out of range for 128-bit varint")
	}
	return out, nil
}

func SizeVarInt128(v *BigInt) (int, error) {
	norm, err := normalizeUnsigned128(v)
	if err != nil {
		return 0, err
	}
	if norm.Sign() == 0 {
		return 1, nil
	}
	size := 0
	tmp := new(big.Int).Set(norm)
	for tmp.Sign() != 0 {
		tmp.Rsh(tmp, 7)
		size++
	}
	return size, nil
}

func AppendVarInt128(dst []byte, v *BigInt) ([]byte, error) {
	norm, err := normalizeUnsigned128(v)
	if err != nil {
		return nil, err
	}
	if norm.Sign() == 0 {
		return append(dst, 0), nil
	}
	tmp := new(big.Int).Set(norm)
	mask := big.NewInt(0x7f)
	for tmp.Sign() != 0 {
		part := new(big.Int).And(tmp, mask).Uint64()
		tmp.Rsh(tmp, 7)
		b := byte(part)
		if tmp.Sign() != 0 {
			b |= 0x80
		}
		dst = append(dst, b)
	}
	return dst, nil
}

func DecodeVarInt128(src []byte, out **BigInt) ([]byte, error) {
	value := big.NewInt(0)
	for i := 0; i < 19; i++ {
		if len(src) <= i {
			return nil, io.ErrUnexpectedEOF
		}
		b := src[i]
		part := big.NewInt(int64(b & 0x7f))
		part.Lsh(part, uint(i*7))
		value.Or(value, part)
		if b&0x80 == 0 {
			*out = value
			return src[i+1:], nil
		}
	}
	return nil, fmt.Errorf("varint128 too large")
}

func SizeSizedUint(v *BigInt, size int) (int, error) {
	if size < 0 {
		return 0, fmt.Errorf("negative int size")
	}
	if v == nil {
		return size, nil
	}
	if v.Sign() < 0 {
		return 0, fmt.Errorf("fixed int must be unsigned")
	}
	if v.BitLen() > size*8 {
		return 0, fmt.Errorf("value out of range for %d-byte int", size)
	}
	return size, nil
}

func AppendSizedUint(dst []byte, v *BigInt, size int) ([]byte, error) {
	if _, err := SizeSizedUint(v, size); err != nil {
		return nil, err
	}
	buf := make([]byte, size)
	if v != nil {
		b := v.Bytes()
		copy(buf[size-len(b):], b)
	}
	return append(dst, buf...), nil
}

func DecodeSizedUint(src []byte, out **BigInt, size int) ([]byte, error) {
	if size < 0 {
		return nil, fmt.Errorf("negative int size")
	}
	if len(src) < size {
		return nil, io.ErrUnexpectedEOF
	}
	value := new(big.Int).SetBytes(src[:size])
	*out = value
	return src[size:], nil
}

func SizeLPVec3(v LPVec3) (int, error) {
	max := math.Max(math.Abs(v.X), math.Max(math.Abs(v.Y), math.Abs(v.Z)))
	if max < lpVec3AbsMinValue {
		return 1, nil
	}
	scale := int32(math.Ceil(max))
	size := 6
	if (scale & 3) != scale {
		n, err := SizeVarInt(scale / 4)
		if err != nil {
			return 0, err
		}
		size += n
	}
	return size, nil
}

func AppendLPVec3(dst []byte, v LPVec3) ([]byte, error) {
	x := sanitizeLPVec3Value(v.X)
	y := sanitizeLPVec3Value(v.Y)
	z := sanitizeLPVec3Value(v.Z)

	max := math.Max(math.Abs(x), math.Max(math.Abs(y), math.Abs(z)))
	if max < lpVec3AbsMinValue {
		return append(dst, 0), nil
	}

	scale := int32(math.Ceil(max))
	needsContinuation := (scale & 3) != scale
	scaleByte := byte(scale & 3)
	if needsContinuation {
		scaleByte |= 4
	}

	px := packLPVec3Value(x / float64(scale))
	py := packLPVec3Value(y / float64(scale))
	pz := packLPVec3Value(z / float64(scale))

	low32 := uint32(scaleByte) | uint32(px<<3) | uint32(py<<18)
	high16 := uint16((py >> 14) & 0x01)
	high16 |= uint16(pz << 1)

	dst = appendFixed(dst, 6, func(buf []byte) {
		binary.LittleEndian.PutUint32(buf[:4], low32)
		binary.LittleEndian.PutUint16(buf[4:6], high16)
	})
	if needsContinuation {
		var err error
		dst, err = AppendVarInt(dst, scale/4)
		if err != nil {
			return nil, err
		}
	}
	return dst, nil
}

func DecodeLPVec3(src []byte, out *LPVec3) ([]byte, error) {
	if len(src) < 1 {
		return nil, io.ErrUnexpectedEOF
	}
	a := src[0]
	if a == 0 {
		*out = LPVec3{}
		return src[1:], nil
	}
	if len(src) < 6 {
		return nil, io.ErrUnexpectedEOF
	}

	b := src[1]
	c := binary.LittleEndian.Uint32(src[2:6])
	packed := (uint64(c) << 16) | (uint64(b) << 8) | uint64(a)

	scale := float64(a & 3)
	rest := src[6:]
	if a&4 == 4 {
		var extra int32
		var err error
		rest, err = DecodeVarInt(rest, &extra)
		if err != nil {
			return nil, err
		}
		scale = float64(extra*4 + int32(a&3))
	}

	out.X = unpackLPVec3Value(packed, 3) * scale
	out.Y = unpackLPVec3Value(packed, 18) * scale
	out.Z = unpackLPVec3Value(packed, 33) * scale
	return rest, nil
}

func SizeString(v string) (int, error) {
	n, _ := SizeVarInt(int32(len(v)))
	return n + len(v), nil
}

func AppendString(dst []byte, v string) ([]byte, error) {
	var err error
	dst, err = AppendVarInt(dst, int32(len(v)))
	if err != nil {
		return nil, err
	}
	return append(dst, v...), nil
}

func DecodeString(src []byte, out *string) ([]byte, error) {
	var l int32
	rest, err := DecodeVarInt(src, &l)
	if err != nil {
		return nil, err
	}
	if l < 0 || len(rest) < int(l) {
		return nil, io.ErrUnexpectedEOF
	}
	*out = string(rest[:l])
	return rest[l:], nil
}

func SizeByteArray(v []byte) (int, error) {
	n, _ := SizeVarInt(int32(len(v)))
	return n + len(v), nil
}

func AppendByteArray(dst []byte, v []byte) ([]byte, error) {
	var err error
	dst, err = AppendVarInt(dst, int32(len(v)))
	if err != nil {
		return nil, err
	}
	return append(dst, v...), nil
}

func DecodeByteArray(src []byte, out *[]byte) ([]byte, error) {
	var l int32
	rest, err := DecodeVarInt(src, &l)
	if err != nil {
		return nil, err
	}
	if l < 0 || len(rest) < int(l) {
		return nil, io.ErrUnexpectedEOF
	}
	if cap(*out) < int(l) {
		*out = make([]byte, int(l))
	} else {
		*out = (*out)[:int(l)]
	}
	copy(*out, rest[:l])
	return rest[l:], nil
}

func SizeRestBuffer(v []byte) (int, error) { return len(v), nil }

func AppendRestBuffer(dst []byte, v []byte) ([]byte, error) {
	return append(dst, v...), nil
}

func DecodeRestBuffer(src []byte, out *[]byte) ([]byte, error) {
	if cap(*out) < len(src) {
		*out = append(make([]byte, 0, len(src)), src...)
	} else {
		*out = (*out)[:len(src)]
		copy(*out, src)
	}
	return src[len(src):], nil
}

func SizeRawBytes(v []byte) (int, error) { return len(v), nil }

func AppendRawBytes(dst []byte, v []byte) ([]byte, error) {
	return append(dst, v...), nil
}

func DecodeFixedBytes(src []byte, out *[]byte, n int) ([]byte, error) {
	if n < 0 || len(src) < n {
		return nil, io.ErrUnexpectedEOF
	}
	if cap(*out) < n {
		*out = make([]byte, n)
	} else {
		*out = (*out)[:n]
	}
	copy(*out, src[:n])
	return src[n:], nil
}

func SizeCString(v string, encoding string) (int, error) {
	b, err := encodeText(v, encoding)
	if err != nil {
		return 0, err
	}
	return len(b) + terminatorLen(encoding), nil
}

func AppendCString(dst []byte, v string, encoding string) ([]byte, error) {
	b, err := encodeText(v, encoding)
	if err != nil {
		return nil, err
	}
	dst = append(dst, b...)
	for i := 0; i < terminatorLen(encoding); i++ {
		dst = append(dst, 0)
	}
	return dst, nil
}

func DecodeCString(src []byte, out *string, encoding string) ([]byte, error) {
	term := terminatorLen(encoding)
	switch term {
	case 1:
		idx := bytes.IndexByte(src, 0)
		if idx < 0 {
			return nil, io.ErrUnexpectedEOF
		}
		text, err := decodeText(src[:idx], encoding)
		if err != nil {
			return nil, err
		}
		*out = text
		return src[idx+1:], nil
	case 2:
		for i := 0; i+1 < len(src); i += 2 {
			if src[i] == 0 && src[i+1] == 0 {
				text, err := decodeText(src[:i], encoding)
				if err != nil {
					return nil, err
				}
				*out = text
				return src[i+2:], nil
			}
		}
		return nil, io.ErrUnexpectedEOF
	default:
		return nil, fmt.Errorf("unsupported cstring terminator")
	}
}

func SizeCStringUTF8(v string) (int, error)                  { return SizeCString(v, "utf-8") }
func AppendCStringUTF8(dst []byte, v string) ([]byte, error) { return AppendCString(dst, v, "utf-8") }
func DecodeCStringUTF8(src []byte, out *string) ([]byte, error) {
	return DecodeCString(src, out, "utf-8")
}

func SizeStringEncoded(v string, encoding string) (int, error) {
	b, err := encodeText(v, encoding)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

func AppendStringEncoded(dst []byte, v string, encoding string) ([]byte, error) {
	b, err := encodeText(v, encoding)
	if err != nil {
		return nil, err
	}
	return append(dst, b...), nil
}

func DecodeFixedString(src []byte, out *string, n int, encoding string) ([]byte, error) {
	if n < 0 || len(src) < n {
		return nil, io.ErrUnexpectedEOF
	}
	text, err := decodeText(src[:n], encoding)
	if err != nil {
		return nil, err
	}
	*out = text
	return src[n:], nil
}

func encodeText(v string, encoding string) ([]byte, error) {
	switch normalizeEncoding(encoding) {
	case "", "utf-8", "utf8":
		return []byte(v), nil
	case "utf-16", "utf-16be":
		return encodeUTF16(v, binary.BigEndian), nil
	case "utf-16le":
		return encodeUTF16(v, binary.LittleEndian), nil
	default:
		return nil, fmt.Errorf("unsupported string encoding %q", encoding)
	}
}

func decodeText(v []byte, encoding string) (string, error) {
	switch normalizeEncoding(encoding) {
	case "", "utf-8", "utf8":
		if !utf8.Valid(v) {
			return "", fmt.Errorf("invalid utf-8")
		}
		return string(v), nil
	case "utf-16", "utf-16be":
		return decodeUTF16(v, binary.BigEndian)
	case "utf-16le":
		return decodeUTF16(v, binary.LittleEndian)
	default:
		return "", fmt.Errorf("unsupported string encoding %q", encoding)
	}
}

func encodeUTF16(v string, order binary.ByteOrder) []byte {
	u := utf16.Encode([]rune(v))
	out := make([]byte, len(u)*2)
	for i, r := range u {
		order.PutUint16(out[i*2:], r)
	}
	return out
}

func decodeUTF16(v []byte, order binary.ByteOrder) (string, error) {
	if len(v)%2 != 0 {
		return "", fmt.Errorf("invalid utf-16 byte length")
	}
	u := make([]uint16, len(v)/2)
	for i := range u {
		u[i] = order.Uint16(v[i*2:])
	}
	return string(utf16.Decode(u)), nil
}

func normalizeEncoding(encoding string) string {
	return strings.ToLower(strings.TrimSpace(encoding))
}

func terminatorLen(encoding string) int {
	switch normalizeEncoding(encoding) {
	case "utf-16", "utf-16be", "utf-16le":
		return 2
	default:
		return 1
	}
}

func SizeUUID(uuid.UUID) (int, error) { return 16, nil }

func AppendUUID(dst []byte, v uuid.UUID) ([]byte, error) {
	return append(dst, v[:]...), nil
}

func DecodeUUID(src []byte, out *uuid.UUID) ([]byte, error) {
	if len(src) < 16 {
		return nil, io.ErrUnexpectedEOF
	}
	copy((*out)[:], src[:16])
	return src[16:], nil
}

func AppendField[T WriterTo](dst []byte, v T) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := v.WriteTo(&buf); err != nil {
		return nil, err
	}
	return append(dst, buf.Bytes()...), nil
}

func SizeField[T WriterTo](v T) (int, error) {
	var buf bytes.Buffer
	if _, err := v.WriteTo(&buf); err != nil {
		return 0, err
	}
	return buf.Len(), nil
}

func DecodeField[T ReaderFrom](src []byte, out T) ([]byte, error) {
	reader := bytes.NewReader(src)
	n, err := out.ReadFrom(reader)
	if err != nil {
		return nil, err
	}
	return src[n:], nil
}

func AppendNBT[T any](dst []byte, v T) ([]byte, error) {
	raw, err := nbtBytes(v)
	if err != nil {
		return nil, err
	}
	return append(dst, raw...), nil
}

func SizeNBT[T any](v T) (int, error) {
	raw, err := nbtBytes(v)
	if err != nil {
		return 0, err
	}
	return len(raw), nil
}

func DecodeNBT[T any](src []byte, out *T) ([]byte, error) {
	n, err := nbtValueLen(src)
	if err != nil {
		return nil, err
	}
	if err := nbt.Unmarshal(src[:n], out); err != nil {
		return nil, err
	}
	return src[n:], nil
}

func SizeOptionalNBT[T any](v *T) (int, error) {
	n, err := SizeBool(v != nil)
	if err != nil || v == nil {
		return n, err
	}
	n2, err := SizeNBT(*v)
	return n + n2, err
}

func AppendOptionalNBT[T any](dst []byte, v *T) ([]byte, error) {
	var err error
	dst, err = AppendBool(dst, v != nil)
	if err != nil || v == nil {
		return dst, err
	}
	return AppendNBT(dst, *v)
}

func DecodeOptionalNBT[T any](src []byte, out **T) ([]byte, error) {
	var present bool
	rest, err := DecodeBool(src, &present)
	if err != nil {
		return nil, err
	}
	if !present {
		*out = nil
		return rest, nil
	}
	value := new(T)
	rest, err = DecodeNBT(rest, value)
	if err != nil {
		return nil, err
	}
	*out = value
	return rest, nil
}

func nbtBytes(v any) ([]byte, error) {
	switch typed := v.(type) {
	case *chat.Message:
		if typed == nil {
			return nil, nil
		}
	case *nbt.RawMessage:
		if typed == nil {
			return nil, nil
		}
	}
	return nbt.Marshal(v)
}

func nbtValueLen(src []byte) (int, error) {
	if len(src) < 1 {
		return 0, io.ErrUnexpectedEOF
	}
	tagType := src[0]
	if tagType == 0 {
		return 1, nil
	}
	if len(src) < 3 {
		return 0, io.ErrUnexpectedEOF
	}
	nameLen := int(binary.BigEndian.Uint16(src[1:3]))
	if len(src) < 3+nameLen {
		return 0, io.ErrUnexpectedEOF
	}
	payloadLen, err := nbtPayloadLen(tagType, src[3+nameLen:])
	if err != nil {
		return 0, err
	}
	return 3 + nameLen + payloadLen, nil
}

func nbtPayloadLen(tagType byte, src []byte) (int, error) {
	switch tagType {
	case 1:
		return requireNBT(src, 1)
	case 2:
		return requireNBT(src, 2)
	case 3, 5:
		return requireNBT(src, 4)
	case 4, 6:
		return requireNBT(src, 8)
	case 7:
		if len(src) < 4 {
			return 0, io.ErrUnexpectedEOF
		}
		n := int(binary.BigEndian.Uint32(src[:4]))
		if n < 0 || len(src) < 4+n {
			return 0, io.ErrUnexpectedEOF
		}
		return 4 + n, nil
	case 8:
		if len(src) < 2 {
			return 0, io.ErrUnexpectedEOF
		}
		n := int(binary.BigEndian.Uint16(src[:2]))
		if len(src) < 2+n {
			return 0, io.ErrUnexpectedEOF
		}
		return 2 + n, nil
	case 9:
		if len(src) < 5 {
			return 0, io.ErrUnexpectedEOF
		}
		elemType := src[0]
		count := int(binary.BigEndian.Uint32(src[1:5]))
		if count < 0 {
			return 0, fmt.Errorf("negative NBT list length")
		}
		offset := 5
		for i := 0; i < count; i++ {
			n, err := nbtPayloadLen(elemType, src[offset:])
			if err != nil {
				return 0, err
			}
			offset += n
		}
		return offset, nil
	case 10:
		offset := 0
		for {
			if len(src[offset:]) < 1 {
				return 0, io.ErrUnexpectedEOF
			}
			childType := src[offset]
			offset++
			if childType == 0 {
				return offset, nil
			}
			if len(src[offset:]) < 2 {
				return 0, io.ErrUnexpectedEOF
			}
			nameLen := int(binary.BigEndian.Uint16(src[offset : offset+2]))
			offset += 2
			if len(src[offset:]) < nameLen {
				return 0, io.ErrUnexpectedEOF
			}
			offset += nameLen
			n, err := nbtPayloadLen(childType, src[offset:])
			if err != nil {
				return 0, err
			}
			offset += n
		}
	case 11:
		if len(src) < 4 {
			return 0, io.ErrUnexpectedEOF
		}
		n := int(binary.BigEndian.Uint32(src[:4]))
		if len(src) < 4+n*4 {
			return 0, io.ErrUnexpectedEOF
		}
		return 4 + n*4, nil
	case 12:
		if len(src) < 4 {
			return 0, io.ErrUnexpectedEOF
		}
		n := int(binary.BigEndian.Uint32(src[:4]))
		if len(src) < 4+n*8 {
			return 0, io.ErrUnexpectedEOF
		}
		return 4 + n*8, nil
	default:
		return 0, fmt.Errorf("unsupported NBT tag type %d", tagType)
	}
}

func requireNBT(src []byte, n int) (int, error) {
	if len(src) < n {
		return 0, io.ErrUnexpectedEOF
	}
	return n, nil
}
