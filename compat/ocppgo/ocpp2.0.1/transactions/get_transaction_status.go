package transactions

import (
	"reflect"

	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/types"
)

// -------------------- Clear Cache (CSMS -> CS) --------------------

const GetTransactionStatusFeatureName = "GetTransactionStatus"

// The field definition of the GetTransactionStatus request payload sent by the CSMS to the Charging Station.
type GetTransactionStatusRequest struct {
	TransactionID string            `json:"transactionId,omitempty" validate:"omitempty,max=36"`
	CustomData    *types.CustomData `json:"customData,omitempty" validate:"omitempty"` // vendor specific extension data (schema customData)
}

// This field definition of the GetTransactionStatus response payload, sent by the Charging Station to the CSMS in response to a GetTransactionStatusRequest.
// In case the request was invalid, or couldn't be processed, an error will be sent instead.
type GetTransactionStatusResponse struct {
	OngoingIndicator *bool             `json:"ongoingIndicator,omitempty" validate:"omitempty"`
	MessagesInQueue  bool              `json:"messagesInQueue"`
	CustomData       *types.CustomData `json:"customData,omitempty" validate:"omitempty"` // vendor specific extension data (schema customData)
}

// In some scenarios a CSMS needs to know whether there are still messages for a transaction that need to be delivered.
// The CSMS shall ask if the Charging Station has still messages in the queue for this transaction with the GetTransactionStatusRequest.
// It may optionally specify a transactionId, to know if a transaction is still ongoing.
// Upon receiving a GetTransactionStatusRequest, the Charging Station shall respond with a GetTransactionStatusResponse payload.
type GetTransactionStatusFeature struct{}

func (f GetTransactionStatusFeature) GetFeatureName() string {
	return GetTransactionStatusFeatureName
}

func (f GetTransactionStatusFeature) GetRequestType() reflect.Type {
	return reflect.TypeOf(GetTransactionStatusRequest{})
}

func (f GetTransactionStatusFeature) GetResponseType() reflect.Type {
	return reflect.TypeOf(GetTransactionStatusResponse{})
}

func (r GetTransactionStatusRequest) GetFeatureName() string {
	return GetTransactionStatusFeatureName
}

func (c GetTransactionStatusResponse) GetFeatureName() string {
	return GetTransactionStatusFeatureName
}

// Creates a new GetTransactionStatusRequest, which doesn't contain any required or optional fields.
func NewGetTransactionStatusRequest() *GetTransactionStatusRequest {
	return &GetTransactionStatusRequest{}
}

// Creates a new GetTransactionStatusResponse, containing all required fields. There are no optional fields for this message.
func NewGetTransactionStatusResponse(messagesInQueue bool) *GetTransactionStatusResponse {
	return &GetTransactionStatusResponse{MessagesInQueue: messagesInQueue}
}
