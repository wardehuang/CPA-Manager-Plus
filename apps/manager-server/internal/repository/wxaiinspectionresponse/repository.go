package wxaiinspectionresponse

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
)

type Repository interface {
	Insert(ctx context.Context, response model.WxaiInspectionHTTPResponse) (model.WxaiInspectionHTTPResponse, error)
}

type repository struct {
	database *sql.DB
}

func New(database *sql.DB) Repository {
	return &repository{database: database}
}

func (repository *repository) Insert(
	ctx context.Context,
	response model.WxaiInspectionHTTPResponse,
) (model.WxaiInspectionHTTPResponse, error) {
	if response.CreatedAtMS <= 0 {
		response.CreatedAtMS = time.Now().UnixMilli()
	}
	responseHeadersJSON, err := json.Marshal(response.ResponseHeaders)
	if err != nil {
		return model.WxaiInspectionHTTPResponse{}, fmt.Errorf("marshal response headers: %w", err)
	}
	databaseResult, err := repository.database.ExecContext(
		ctx,
		`insert into wxai_inspection_http_responses (
			run_id, account_key, file_name, request_stage, request_method, request_url,
			response_status_code, final_url, response_headers_json, response_body, body_truncated,
			sensitive_fields_redacted, created_at_ms
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		response.RunID,
		response.AccountKey,
		response.FileName,
		response.RequestStage,
		response.RequestMethod,
		response.RequestURL,
		response.ResponseStatusCode,
		nullableString(response.FinalURL),
		string(responseHeadersJSON),
		response.ResponseBody,
		boolInteger(response.BodyTruncated),
		boolInteger(response.SensitiveFieldsRedacted),
		response.CreatedAtMS,
	)
	if err != nil {
		return model.WxaiInspectionHTTPResponse{}, err
	}
	response.ID, err = databaseResult.LastInsertId()
	if err != nil {
		return model.WxaiInspectionHTTPResponse{}, err
	}
	return response, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}
