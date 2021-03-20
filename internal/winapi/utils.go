package winapi

import (
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Uint16BufferToSlice wraps a uint16 pointer-and-length into a slice
// for easier interop with Go APIs
func Uint16BufferToSlice(buffer *uint16, bufferLength int) (result []uint16) {
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&result))
	hdr.Data = uintptr(unsafe.Pointer(buffer))
	hdr.Cap = bufferLength
	hdr.Len = bufferLength

	return
}

type UnicodeString struct {
	Length        uint16
	MaximumLength uint16
	Buffer        *uint16
}

//String converts a UnicodeString to a golang string
func (uni UnicodeString) String() string {
	// UnicodeString is not guaranteed to be null terminated, therefore
	// use the UnicodeString's Length field
	return syscall.UTF16ToString(Uint16BufferToSlice(uni.Buffer, int(uni.Length/2)))
}

// NewUnicodeString allocates a new UnicodeString and copies `s` into
// the buffer of the new UnicodeString.
func NewUnicodeString(s string) (*UnicodeString, error) {
	// Get length of original `s` to use in the UnicodeString since the `buf`
	// created later will have an additional trailing null character
	length := len(s)
	if length > 32767 {
		return nil, syscall.ENAMETOOLONG
	}

	buf, err := windows.UTF16FromString(s)
	if err != nil {
		return nil, err
	}
	uni := &UnicodeString{
		Length:        uint16(length * 2),
		MaximumLength: uint16(length * 2),
		Buffer:        &buf[0],
	}
	return uni, nil
}

// ConvertStringSetToSlice is a helper function used to convert the contents of
// `buf` into a string slice. `buf` contains a set of null terminated strings
// with an additional null at the end to indicate the end of the set.
func ConvertStringSetToSlice(buf []byte) ([]string, error) {
	var results []string
	prev := 0
	for i := range buf {
		if buf[i] == 0 {
			if prev == i {
				// found two null characters in a row, return result
				return results, nil
			}
			results = append(results, string(buf[prev:i]))
			prev = i + 1
		}
	}
	return nil, errors.New("string set malformed: missing null terminator at end of buffer")
}

// parseUtf16LE parses a UTF-16LE byte array into a string (without passing
// through a uint16 or rune array).
func ParseUtf16LE(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b) / 2)
	for len(b) > 0 {
		r := rune(binary.LittleEndian.Uint16(b))
		if utf16.IsSurrogate(r) && len(b) > 2 {
			sb.WriteRune(utf16.DecodeRune(r, rune(binary.LittleEndian.Uint16(b[2:]))))
			b = b[4:]
		} else {
			sb.WriteRune(r)
			b = b[2:]
		}
	}
	return sb.String()
}
