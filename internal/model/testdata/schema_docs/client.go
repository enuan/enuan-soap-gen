// Hand-written stand-in for the client that soap-go generates from
// definitions.wsdl. Only the shapes the model package inspects are
// reproduced; the fixture lives under testdata, so it is never compiled.
package schemadocs

import (
	"context"
	"encoding/xml"
)

type Client struct{}

func (c *Client) GetVehicle(ctx context.Context, req *GetVehicleWrapper, opts ...func() error) (*GetVehicleResponseWrapper, error) {
	return nil, nil
}

type GetVehicleWrapper struct {
	XMLName xml.Name `xml:"urn:test:schemadocs GetVehicle"`
	Plate   *string  `xml:"plate,omitempty"`
	Vin     *string  `xml:"vin,omitempty"`
	Year    *int32   `xml:"year,omitempty"`
}

type GetVehicleResponseWrapper struct {
	XMLName xml.Name `xml:"urn:test:schemadocs GetVehicleResponse"`
	Result  *string  `xml:"result,omitempty"`
}
