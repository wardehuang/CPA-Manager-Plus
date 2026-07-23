package model

type WxaiInspectionHTTPResponse struct {
	ID                      int64               `json:"id"`
	RunID                   int64               `json:"runId"`
	AccountKey              string              `json:"accountKey"`
	FileName                string              `json:"fileName"`
	RequestStage            string              `json:"requestStage"`
	RequestMethod           string              `json:"requestMethod"`
	RequestURL              string              `json:"requestUrl"`
	ResponseStatusCode      int                 `json:"responseStatusCode"`
	FinalURL                string              `json:"finalUrl,omitempty"`
	ResponseHeaders         map[string][]string `json:"responseHeaders,omitempty"`
	ResponseBody            []byte              `json:"responseBody"`
	BodyTruncated           bool                `json:"bodyTruncated"`
	SensitiveFieldsRedacted bool                `json:"sensitiveFieldsRedacted"`
	CreatedAtMS             int64               `json:"createdAtMs"`
}
