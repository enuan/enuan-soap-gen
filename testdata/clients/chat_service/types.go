package chat_service

import (
	"encoding/xml"
)

// EchoWrapper represents the Echo element
type EchoWrapper struct {
	XMLName      xml.Name `xml:"urn:example:ns1 Echo"`
	InputMessage *string  `xml:"inputMessage,omitempty"`
}

// EchoResponseWrapper represents the EchoResponse element
type EchoResponseWrapper struct {
	XMLName    xml.Name `xml:"urn:example:ns1 EchoResponse"`
	EchoResult *string  `xml:"EchoResult,omitempty"`
}

// SubmitTravelRequestWrapper represents the SubmitTravelRequest element
type SubmitTravelRequestWrapper struct {
	XMLName    xml.Name `xml:"urn:example:ns1 SubmitTravelRequest"`
	XmlRequest *string  `xml:"xmlRequest,omitempty"`
}

// SubmitTravelRequestResponseWrapper represents the SubmitTravelRequestResponse element
type SubmitTravelRequestResponseWrapper struct {
	XMLName                   xml.Name `xml:"urn:example:ns1 SubmitTravelRequestResponse"`
	SubmitTravelRequestResult *string  `xml:"SubmitTravelRequestResult,omitempty"`
}

// ApproveTravelResponseWrapper represents the ApproveTravelResponse element
type ApproveTravelResponseWrapper struct {
	XMLName    xml.Name `xml:"urn:example:ns1 ApproveTravelResponse"`
	XmlRequest *string  `xml:"xmlRequest,omitempty"`
}

// ApproveTravelResponseResponseWrapper represents the ApproveTravelResponseResponse element
type ApproveTravelResponseResponseWrapper struct {
	XMLName                     xml.Name `xml:"urn:example:ns1 ApproveTravelResponseResponse"`
	ApproveTravelResponseResult *string  `xml:"ApproveTravelResponseResult,omitempty"`
}

// ApproveHotelResponseWrapper represents the ApproveHotelResponse element
type ApproveHotelResponseWrapper struct {
	XMLName          xml.Name `xml:"urn:example:ns1 ApproveHotelResponse"`
	XmlApprovalHotel *string  `xml:"xmlApprovalHotel,omitempty"`
}

// ApproveHotelResponseResponseWrapper represents the ApproveHotelResponseResponse element
type ApproveHotelResponseResponseWrapper struct {
	XMLName                    xml.Name `xml:"urn:example:ns1 ApproveHotelResponseResponse"`
	ApproveHotelResponseResult *string  `xml:"ApproveHotelResponseResult,omitempty"`
}
