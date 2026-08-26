package engine

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ResolveChartConfig replaces "$column_name" references in a Chart.js JSON config
// with actual data arrays from the query results.
func ResolveChartConfig(vizConfig string, columns []string, rows [][]interface{}) (string, error) {
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(vizConfig), &config); err != nil {
		return "", fmt.Errorf("invalid viz_config JSON: %w", err)
	}

	colIndex := make(map[string]int)
	for i, col := range columns {
		colIndex[NormalizeColRef(col)] = i
	}

	resolved := resolveRefs(config, colIndex, rows)
	resolvedMap, _ := resolved.(map[string]interface{})

	if typeStr, _ := resolvedMap["type"].(string); typeStr == "scatter" {
		if data, ok := resolvedMap["data"].(map[string]interface{}); ok {
			if datasets, ok := data["datasets"].([]interface{}); ok {
				for _, ds := range datasets {
					if dsMap, ok := ds.(map[string]interface{}); ok {
						if dsData, ok := dsMap["data"].([]interface{}); ok {
							points := zipScatterPoints(dsData, colIndex, rows)
							if points != nil {
								dsMap["data"] = points
							}
						}
					}
				}
			}
		}
	}

	out, err := json.Marshal(resolved)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// zipScatterPoints converts [{"x":"$col1","y":"$col2"}] into [{x:v1,y:v2}, ...]
func zipScatterPoints(items []interface{}, colIndex map[string]int, rows [][]interface{}) []interface{} {
	if len(items) == 0 {
		return nil
	}
	first, ok := items[0].(map[string]interface{})
	if !ok {
		return nil
	}
	xArr, xOk := first["x"].([]interface{})
	yArr, yOk := first["y"].([]interface{})
	if !xOk || !yOk {
		return nil
	}
	n := len(xArr)
	if len(yArr) < n {
		n = len(yArr)
	}
	points := make([]interface{}, n)
	for i := 0; i < n; i++ {
		points[i] = map[string]interface{}{"x": xArr[i], "y": yArr[i]}
	}
	return points
}

// resolveRefs walks a JSON-like tree and replaces "$column_name" strings
// with the actual column data arrays from the query rows.
func resolveRefs(node interface{}, colIndex map[string]int, rows [][]interface{}) interface{} {
	switch v := node.(type) {
	case string:
		if strings.HasPrefix(v, "$") {
			colName := NormalizeColRef(v[1:])
			if idx, ok := colIndex[colName]; ok {
				data := make([]interface{}, len(rows))
				for i, row := range rows {
					if idx < len(row) {
						data[i] = row[idx]
					}
				}
				return data
			}
		}
		return v
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, val := range v {
			result[k] = resolveRefs(val, colIndex, rows)
		}
		return result
	case []interface{}:
		result := make([]interface{}, 0, len(v))
		for _, val := range v {
			resolved := resolveRefs(val, colIndex, rows)
			if str, ok := val.(string); ok && strings.HasPrefix(str, "$") {
				if arr, ok := resolved.([]interface{}); ok && len(arr) > 0 {
					result = append(result, arr...)
					continue
				}
			}
			result = append(result, resolved)
		}
		return result
	default:
		return v
	}
}

// MustMarshalJSON marshals a value to JSON or returns an empty object on error.
func MustMarshalJSON(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return data
}

// NormalizeColRef lowercases and strips non-alphanumeric characters.
func NormalizeColRef(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
