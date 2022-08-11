package test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bnb-chain/zkbas/common/util"
	"github.com/bnb-chain/zkbas/service/api/app/internal/types"
)

func (s *AppSuite) TestSearch() {
	type args struct {
		info string
	}
	tests := []struct {
		name     string
		args     args
		httpCode int
		dataType int32
	}{
		{"search block", args{"1"}, 200, util.TypeBlockHeight},
		{"search account", args{"gas.legend"}, 200, util.TypeAccountName},
		{"not found", args{"notexist"}, 400, 0},
	}

	for _, tt := range tests {
		s.T().Run(tt.name, func(t *testing.T) {
			httpCode, result := Search(s, tt.args.info)
			assert.Equal(t, tt.httpCode, httpCode)
			if httpCode == http.StatusOK {
				assert.NotNil(t, result.DataType)
				assert.Equal(t, tt.dataType, result.DataType)
				fmt.Printf("result: %+v \n", result)
			}
		})
	}

}

func Search(s *AppSuite, info string) (int, *types.RespSearch) {
	resp, err := http.Get(s.url + "/api/v1/info/search?info=" + info)
	assert.NoError(s.T(), err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	assert.NoError(s.T(), err)

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	result := types.RespSearch{}
	err = json.Unmarshal(body, &result)
	return resp.StatusCode, &result
}