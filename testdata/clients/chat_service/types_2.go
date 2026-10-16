package chat_service

import (
	"encoding/xml"
	soap "github.com/enuan/soap-go"
)

// Simple types

// Char represents the char simpleType (restricting xs:int)
type Char int32

// Duration represents the duration simpleType (restricting xs:duration)
type Duration string

// Guid represents the guid simpleType (restricting xs:string)
type Guid string

// AnyTypeWrapper represents the anyType element
type AnyTypeWrapper struct {
	XMLName xml.Name `xml:"anyType"`
	Value   string   `xml:",chardata"`
}

// AnyURIWrapper represents the anyURI element
type AnyURIWrapper struct {
	XMLName xml.Name `xml:"anyURI"`
	Value   string   `xml:",chardata"`
}

// Base64BinaryWrapper represents the base64Binary element
type Base64BinaryWrapper struct {
	XMLName xml.Name `xml:"base64Binary"`
	Value   []byte   `xml:",chardata"`
}

// BooleanWrapper represents the boolean element
type BooleanWrapper struct {
	XMLName xml.Name `xml:"boolean"`
	Value   bool     `xml:",chardata"`
}

// ByteWrapper represents the byte element
type ByteWrapper struct {
	XMLName xml.Name `xml:"byte"`
	Value   int8     `xml:",chardata"`
}

// DateTimeWrapper represents the dateTime element
type DateTimeWrapper struct {
	XMLName xml.Name         `xml:"dateTime"`
	Value   soap.XSDDateTime `xml:",chardata"`
}

// DecimalWrapper represents the decimal element
type DecimalWrapper struct {
	XMLName xml.Name `xml:"decimal"`
	Value   string   `xml:",chardata"`
}

// DoubleWrapper represents the double element
type DoubleWrapper struct {
	XMLName xml.Name `xml:"double"`
	Value   float64  `xml:",chardata"`
}

// FloatWrapper represents the float element
type FloatWrapper struct {
	XMLName xml.Name `xml:"float"`
	Value   float64  `xml:",chardata"`
}

// IntWrapper represents the int element
type IntWrapper struct {
	XMLName xml.Name `xml:"int"`
	Value   int32    `xml:",chardata"`
}

// LongWrapper represents the long element
type LongWrapper struct {
	XMLName xml.Name `xml:"long"`
	Value   int64    `xml:",chardata"`
}

// QNameWrapper represents the QName element
type QNameWrapper struct {
	XMLName xml.Name `xml:"QName"`
	Value   xml.Name `xml:",chardata"`
}

// ShortWrapper represents the short element
type ShortWrapper struct {
	XMLName xml.Name `xml:"short"`
	Value   int16    `xml:",chardata"`
}

// StringWrapper represents the string element
type StringWrapper struct {
	XMLName xml.Name `xml:"string"`
	Value   string   `xml:",chardata"`
}

// UnsignedByteWrapper represents the unsignedByte element
type UnsignedByteWrapper struct {
	XMLName xml.Name `xml:"unsignedByte"`
	Value   uint8    `xml:",chardata"`
}

// UnsignedIntWrapper represents the unsignedInt element
type UnsignedIntWrapper struct {
	XMLName xml.Name `xml:"unsignedInt"`
	Value   uint32   `xml:",chardata"`
}

// UnsignedLongWrapper represents the unsignedLong element
type UnsignedLongWrapper struct {
	XMLName xml.Name `xml:"unsignedLong"`
	Value   uint64   `xml:",chardata"`
}

// UnsignedShortWrapper represents the unsignedShort element
type UnsignedShortWrapper struct {
	XMLName xml.Name `xml:"unsignedShort"`
	Value   uint16   `xml:",chardata"`
}

// CharWrapper represents the char element
type CharWrapper struct {
	XMLName xml.Name `xml:"char"`
	Value   int32    `xml:",chardata"`
}

// DurationWrapper represents the duration element
type DurationWrapper struct {
	XMLName xml.Name `xml:"duration"`
	Value   string   `xml:",chardata"`
}

// GuidWrapper represents the guid element
type GuidWrapper struct {
	XMLName xml.Name `xml:"guid"`
	Value   string   `xml:",chardata"`
}
