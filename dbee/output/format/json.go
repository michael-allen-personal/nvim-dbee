package format

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/kndndrj/nvim-dbee/dbee/models"
	"github.com/kndndrj/nvim-dbee/dbee/output"
)

var _ output.Formatter = (*JSON)(nil)

type JSON struct{}

func NewJSON() *JSON {
	return &JSON{}
}

func (jf *JSON) Name() string {
	return "json"
}

func (jf *JSON) parseSchemaFul(result models.IterResult) ([]map[string]any, error) {
	var data []map[string]any

	header, err := result.Header()
	if err != nil {
		return nil, err
	}

	for {
		row, err := result.Next()
		if err != nil {
			return nil, err
		}
		if row == nil {
			break
		}

		record := make(map[string]any, len(row))
		for i, val := range row {
			var h string
			if i < len(header) {
				h = header[i]
			} else {
				h = fmt.Sprintf("<unknown-field-%d>", i)
			}
			record[h] = val
		}
		data = append(data, record)
	}
	return data, nil
}

func (jf *JSON) parseSchemaLess(result models.IterResult) ([]any, error) {
	var data []any

	for {
		row, err := result.Next()
		if err != nil {
			return nil, err
		}
		if row == nil {
			break
		}

		if len(row) == 1 {
			data = append(data, row[0])
		} else if len(row) > 1 {
			data = append(data, row)
		}
	}
	return data, nil
}

func (jf *JSON) Format(result models.IterResult, writer io.Writer) error {
	meta, err := result.Meta()
	if err != nil {
		return err
	}

	var data any
	switch meta.SchemaType {
	case models.SchemaLess:
		data, err = jf.parseSchemaLess(result)
	case models.SchemaFul:
		fallthrough
	default:
		data, err = jf.parseSchemaFul(result)
	}

	if err != nil {
		return err
	}

	encoder := json.NewEncoder(writer)
	err = encoder.Encode(data)
	if err != nil {
		return err
	}
	return nil
}
