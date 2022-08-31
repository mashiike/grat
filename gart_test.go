package grat

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConvertQueueURLToARN(t *testing.T) {
	cases := []struct {
		url       string
		errString string
		expected  string
	}{
		{
			url:       "https://localhost:8080/queue",
			errString: "invalid queue url",
		},
		{
			url:      "https://sqs.ap-northeast-1.amazonaws.com/123456789012/dart-example",
			expected: "arn:aws:sqs:ap-northeast-1:123456789012:dart-example",
		},
	}
	for _, c := range cases {
		t.Run(c.url, func(t *testing.T) {
			urlObj, err := url.Parse(c.url)
			require.NoError(t, err)
			arnObj, err := convertQueueURLToARN(urlObj)
			if c.errString == "" {
				require.NoError(t, err)
				require.EqualValues(t, c.expected, arnObj.String())
			} else {
				require.EqualError(t, err, c.errString)
			}

		})
	}
}
