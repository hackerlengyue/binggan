package surge

import (
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strings"

	"time.haomen/binggan/v2/internal/capture"
)

const policyName = "Binggan-Shenzao-Capture"
const markerStart = "# BEGIN BINGGAN SHENZAO CAPTURE"
const markerEnd = "# END BINGGAN SHENZAO CAPTURE"

var ruleSpacing = regexp.MustCompile(`[ \t]*([,()])[ \t]*`)

func sameRule(a, b string) bool {
	return ruleSpacing.ReplaceAllString(a, "$1") == ruleSpacing.ReplaceAllString(b, "$1")
}

// The backend exclusion lets its upstream request use the user's original
// Surge policy. Matching TCP only leaves UDP and all other domains untouched.
func captureRule(executable string) (string, error) {
	if !filepath.IsAbs(executable) || strings.ContainsAny(executable, ",\r\n()*?\"") {
		return "", fmt.Errorf("抓包程序路径不能安全用于 Surge 分流规则")
	}
	return "AND,((DOMAIN-SUFFIX,shenzaokeji.com),(PROTOCOL,TCP),(NOT,((PROCESS-NAME," + executable + "))))," + policyName, nil
}

func patchProfile(original string, d capture.Descriptor) (string, string, error) {
	if strings.Contains(original, markerStart) || strings.Contains(original, policyName) {
		return "", "", fmt.Errorf("Surge 配置中存在同名抓包策略，请先恢复上次连接")
	}
	if err := d.Validate(); err != nil {
		return "", "", err
	}
	host, port, _ := net.SplitHostPort(d.ProxyAddress)
	nl := "\n"
	if strings.Contains(original, "\r\n") {
		nl = "\r\n"
	}
	block := markerStart + nl + policyName + " = socks5, " + host + ", " + port + ", " + d.ProxyUsername + ", " + d.ProxyPassword + nl + markerEnd + nl
	offset := 0
	for _, line := range strings.SplitAfter(original, "\n") {
		offset += len(line)
		if strings.EqualFold(strings.TrimSpace(line), "[Proxy]") {
			if !strings.HasSuffix(line, "\n") {
				block = nl + block
			}
			return original[:offset] + block + original[offset:], block, nil
		}
	}
	// Include the newly created section and separator in the owned block so
	// removing it restores the file byte-for-byte, including its trailing newline.
	block = nl + "[Proxy]" + nl + block
	return original + block, block, nil
}

func removeBlock(current, block string) (string, error) {
	if strings.Count(current, block) != 1 {
		return "", fmt.Errorf("Surge 抓包配置块已被修改，已保留现场；请移除标记的 BINGGAN SHENZAO CAPTURE 配置块")
	}
	return strings.Replace(current, block, "", 1), nil
}
