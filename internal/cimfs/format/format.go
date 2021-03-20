package format

import "github.com/Microsoft/go-winio/pkg/guid"

// PageSize is the alignment of data for large files inside a CIM.
const PageSize = 4096

// RegionOffset encodes an offset to objects as index of the region file
// containing the object and the byte offset within that file.
type RegionOffset uint64

func (o RegionOffset) ByteOffset() uint64 {
	return uint64(o & 0xffffffffffff)
}

func (o RegionOffset) RegionIndex() uint16 {
	return uint16(o >> 48)
}

func NewRegionOffset(off int64, index uint16) RegionOffset {
	return RegionOffset(uint64(index)<<48 | uint64(off))
}

// NullOffset indicates that the specified object does not exist.
const NullOffset = RegionOffset(0)

// Magic specifies the magic number at the beginning of a file.
type Magic [8]uint8

var MagicValue = Magic([8]uint8{'c', 'i', 'm', 'f', 'i', 'l', 'e', '0'})

type Version struct {
	Major, Minor uint32
}

var CurrentVersion = Version{2, 0}

type FileType uint8

const (
	FtImage FileType = iota
	FtRegion
	FtObjectID
)

// CommonHeader is the common header for all CIM-related files.
type CommonHeader struct {
	Magic        Magic
	HeaderLength uint32
	Type         FileType
	Reserved     uint8
	Reserved2    uint16
	Version      Version
	Reserved3    uint64
}

// Region file.
//
// Region files contain all the data and metadata for an image. They are
// arranged as unordered sequences of objects of varying size, and each region
// file type has its own alignment requirement.

const RegionFileName = "region"

// Each region file has a type, and all objects within that file are of the same
// type.
type RegionType uint8

const (
	// All metadata objects (files, directory data, security descriptors, etc.)
	RtMetadata RegionType = 0
	// Page-aligned file data.
	RtData
	// 8-byte aligned file data (for small files).
	RtSmallData
	RtCount
)

// RegionHeader is the header for the region file.
type RegionHeader struct {
	Common    CommonHeader
	Index     uint16
	Type      RegionType
	Reserved  uint8
	Reserved2 uint32
}

// Object ID file
//
// There is an object ID file corresponding to each region file, containing IDs
// for each object that the region file contains. These IDs are not used at
// runtime but are used at write time to deduplicate objects.

const ObjectIdFileName = "objectid"

// ObjectIdHeader is the header for the object ID file.
type ObjectIdHeader struct {
	Common      CommonHeader
	Index       uint16
	Type        RegionType
	Reserved    uint8
	Reserved2   uint32
	TableOffset uint32
	Count       uint32
}

// ObjectID is the object ID itself, containing a length and a digest.
type ObjectID struct {
	Length uint64
	Digest [24]uint8
}

// ObjectIdEntry is an entry in the object ID file. It contains the object ID
// and the byte offset into the corresponding region file.
type ObjectIdEntry struct {
	ObjectID ObjectID
	Offset   uint64
}

type RegionSet struct {
	ID        guid.GUID
	Count     uint16
	Reserved  uint16
	Reserved1 uint32
}

// FilesystemHeader is the header for a filesystem file.
//
// The filesystem file points to the filesystem object inside a region
// file and specifies regions sets.
type FilesystemHeader struct {
	Common           CommonHeader
	Regions          RegionSet
	FilesystemOffset RegionOffset
	Reserved         uint32
	Reserved1        uint16
	ParentCount      uint16
	// RegionSet ParentRegionSets[ParentCount];
}

const UpcaseTableLength = 0x10000 // Only characters in the BMP are upcased

type FileID uint32

// Filesystem specifies a root directory and other metadata necessary to define
// a filesystem.
type Filesystem struct {
	UpcaseTableOffset        RegionOffset
	FileTableDirectoryOffset RegionOffset
	FileTableDirectoryLength uint32
	RootDirectory            FileID
}

// FileTableDirectoryEntry is the entry within a file table directory. Files are
// laid out in a series of file tables, and file tables are specified by a
// directory. The file table directory entry specifies the number of valid files
// within the table, as well as the entry size (which may grow to specify
// additional file metadata in the future).
type FileTableDirectoryEntry struct {
	Offset    RegionOffset
	Count     uint16
	EntrySize uint16
	Reserved  uint32
}

const FilesPerTable = 1024

type StreamType uint16

const (
	StreamTypeData StreamType = iota
	StreamTypeLinkTable
	StreamTypePeImage
)

// Stream describes a stream data and offset. It may point to file data, a link
// table (for directories), or a PeImage object for files that are PE images.
type Stream struct {
	DataOffset    RegionOffset // stream data or PeImage object
	LengthAndType uint64       // 48, 8
}

func (s *Stream) Size() uint64 {
	return (s.LengthAndType & 0xffffffffffff)
}

func (s *Stream) Type() StreamType {
	return StreamType((s.LengthAndType >> 48) & 0xff)
}

func (s *Stream) Sparse() bool {
	return s.LengthAndType&(1<<56) != 0
}

// PeImage is the stream data for a file that is a PE image. It provides an
// on-disk 4KB image mapping for a 512-byte aligned PE image. In this case, the
// image is aligned well on disk for image mappings, but it is discontiguous for
// ordinary reads.
type PeImage struct {
	DataOffset   RegionOffset
	DataLength   uint64
	ImageLength  uint32
	MappingCount uint16
	Flags        uint16 // ValidImage
	// Mappings  []PeImageMapping
}

type PeImageMapping struct {
	FileOffset uint32
	Delta      uint32
}

type FileFlags uint16

const (
	FileFlagReadOnly FileFlags = 1 << iota
	FileFlagHidden
	FileFlagSystem
	FileFlagArchive
	FileFlagSparse
)

// File represents a file in a file system.
type File struct {
	Flags             FileFlags
	EaLength          uint16
	ReparseTag        uint32
	CreationTime      uint64
	LastWriteTime     uint64
	ChangeTime        uint64
	LastAccessTime    uint64
	DefaultStream     Stream       // file default data stream or LinkTable<FileID>
	SdOffset          RegionOffset // uint16 counted gsl::byte[]
	EaOffset          RegionOffset // gsl::byte[]
	ReparseOffset     RegionOffset // uint16 counted gsl::byte[]
	StreamTableOffset RegionOffset // LinkTable<Stream>
	ParentFileId      FileID
	BaseRegionIndex   uint16
	Reserved          uint16
}

const (
	MaximumEaNameLength = 0xfe
	MaximumFullEaLength = 0xffff
)

// Name represents a file's name
type Name struct {
	Length uint16
}

// LinkTable stores either directory entries or alternate data streams.
type LinkTable struct {
	Length    uint32
	LinkCount uint32
}

const (
	MaximumComponentNameLength = 0xff
	MaximumPathLength          = 0x7fff
)
