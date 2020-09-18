package common

import (
	"encoding/json"
	"fmt"

	"github.com/cosmos/cosmos-sdk/client/context"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/bandprotocol/bandchain/chain/x/oracle/types"
)

func getData(cliCtx context.CLIContext, bz []byte, ptr interface{}) error {
	var result types.QueryResult
	if err := json.Unmarshal(bz, &result); err != nil {
		return err
	}
	return cliCtx.Codec.UnmarshalJSON(result.Result, ptr)
}

func queryReporters(route string, cliCtx context.CLIContext, validator sdk.ValAddress) ([]sdk.AccAddress, int64, error) {
	bz, height, err := cliCtx.Query(fmt.Sprintf("custom/%s/%s/%s", route, types.QueryReporters, validator))
	if err != nil {
		return nil, 0, err
	}
	var reporters []sdk.AccAddress
	err = getData(cliCtx, bz, &reporters)
	if err != nil {
		return nil, 0, err
	}
	return reporters, height, nil
}

func queryParams(route string, cliCtx context.CLIContext) (types.Params, int64, error) {
	bz, height, err := cliCtx.Query(fmt.Sprintf("custom/%s/%s", route, types.QueryParams))
	if err != nil {
		return types.Params{}, 0, err
	}
	var params types.Params
	err = getData(cliCtx, bz, &params)
	if err != nil {
		return types.Params{}, 0, err
	}
	return params, height, nil
}

// TODO: Refactor this code with yoda
type VerificationMessage struct {
	ChainID    string           `json:"chain_id"`
	Validator  sdk.ValAddress   `json:"validator"`
	RequestID  types.RequestID  `json:"request_id"`
	ExternalID types.ExternalID `json:"external_id"`
}

func NewVerificationMessage(
	chainID string, validator sdk.ValAddress, requestID types.RequestID, externalID types.ExternalID,
) VerificationMessage {
	return VerificationMessage{
		ChainID:    chainID,
		Validator:  validator,
		RequestID:  requestID,
		ExternalID: externalID,
	}
}

func (msg VerificationMessage) GetSignBytes() []byte {
	return sdk.MustSortJSON(types.ModuleCdc.MustMarshalJSON(msg))
}

func VerifyRequest(
	route string, cliCtx context.CLIContext, chainID string, reporter string,
	validator sdk.ValAddress, requestID types.RequestID, externalID types.ExternalID, signature []byte,
) ([]byte, int64, error) {
	// Verify chain id
	if cliCtx.ChainID != chainID {
		return nil, 0, fmt.Errorf("Chain id doesn't match")
	}

	// Verify signature
	reporterPub, err := sdk.GetPubKeyFromBech32(sdk.Bech32PubKeyTypeAccPub, reporter)
	if err != nil {
		return nil, 0, err
	}
	if !reporterPub.VerifyBytes(
		NewVerificationMessage(
			chainID, validator, requestID, externalID,
		).GetSignBytes(),
		signature,
	) {
		return nil, 0, fmt.Errorf("Signature verification failed")
	}

	// Check reporters
	reporters, _, err := queryReporters(route, cliCtx, validator)
	if err != nil {
		return nil, 0, err
	}
	reporterAddr := sdk.AccAddress(reporterPub.Address())

	isReporter := false
	for _, reporter := range reporters {
		if reporterAddr.Equals(reporter) {
			isReporter = true
		}
	}
	if !isReporter {
		return nil, 0, fmt.Errorf("Invalid reporter")
	}

	request, height, err := queryRequest(route, cliCtx, fmt.Sprintf("%d", requestID))
	if err != nil {
		return nil, 0, err
	}

	// Verify that this validator has been assigned to report this request
	assigned := false
	for _, requestedVal := range request.Request.RequestedValidators {
		if validator.Equals(requestedVal) {
			assigned = true
			break
		}
	}
	if !assigned {
		return nil, 0, fmt.Errorf("Validator has not been assigned in this request")
	}

	// Verify this request need this external id
	requiredExternalData := false
	for _, rawRequest := range request.Request.RawRequests {
		if rawRequest.ExternalID == externalID {
			requiredExternalData = true
			break
		}
	}
	if !requiredExternalData {
		return nil, 0, fmt.Errorf("External id has not been required in this request")
	}

	// Verify validator hasn't reported on the request.
	reported := false
	for _, report := range request.Reports {
		if report.Validator.Equals(validator) {
			reported = true
			break
		}
	}

	if reported {
		return nil, 0, fmt.Errorf("Validator has reported on this request")
	}

	// Verify request has not been expired
	params, _, err := queryParams(route, cliCtx)
	if err != nil {
		return nil, 0, err
	}

	if request.Request.RequestHeight+int64(params.ExpirationBlockCount) < height {
		return nil, 0, fmt.Errorf("Request has been expired")
	}
	bz, err := types.QueryOK(true)
	return bz, height, err
}