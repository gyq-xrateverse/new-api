package volcengine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"

	channelconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const (
	contextKeyTTSRequest     = "volcengine_tts_request"
	contextKeyResponseFormat = "response_format"
)

// 安全地截断字符串用于日志输出
func safeTruncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

type Adaptor struct {
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, req *dto.ClaudeRequest) (any, error) {
	adaptor := openai.Adaptor{}
	return adaptor.ConvertClaudeRequest(c, info, req)
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	fmt.Printf("\n🔍 [ConvertAudioRequest] 开始处理TTS请求\n")
	fmt.Printf("🔍 [ConvertAudioRequest] RelayMode: %d\n", info.RelayMode)

	if info.RelayMode != constant.RelayModeAudioSpeech {
		return nil, errors.New("unsupported audio relay mode")
	}

	fmt.Printf("🔍 [ConvertAudioRequest] API Key: %s\n", safeTruncate(info.ApiKey, 20))
	appID, token, err := parseVolcengineAuth(info.ApiKey)
	if err != nil {
		fmt.Printf("❌ [ConvertAudioRequest] API Key解析失败: %v\n", err)
		return nil, err
	}
	fmt.Printf("🔍 [ConvertAudioRequest] AppID: %s, Token: %s\n", appID, safeTruncate(token, 20))

	voiceType := mapVoiceType(request.Voice)
	speedRatio := request.Speed
	encoding := mapEncoding(request.ResponseFormat)

	fmt.Printf("🔍 [ConvertAudioRequest] 基础参数:\n")
	fmt.Printf("  - Voice: %s -> %s\n", request.Voice, voiceType)
	fmt.Printf("  - Speed: %.2f\n", speedRatio)
	fmt.Printf("  - Format: %s -> %s\n", request.ResponseFormat, encoding)
	fmt.Printf("  - Input Text: %s\n", request.Input)
	fmt.Printf("  - OriginModelName: %s\n", info.OriginModelName)
	fmt.Printf("  - Metadata Length: %d bytes\n", len(request.Metadata))
	if len(request.Metadata) > 0 {
		fmt.Printf("  - Metadata Raw: %s\n", string(request.Metadata))
	}

	c.Set(contextKeyResponseFormat, encoding)

	volcRequest := VolcengineTTSRequest{
		App: VolcengineTTSApp{
			AppID:   appID,
			Token:   "access_token",  // 🔧 豆包要求这里必须是固定字符串 "access_token",不是实际的 token
			Cluster: "volcano_tts",
		},
		User: VolcengineTTSUser{
			UID: "openai_relay_user",
		},
		Audio: VolcengineTTSAudio{
			VoiceType:  voiceType,
			Encoding:   encoding,
			SpeedRatio: speedRatio,
			Rate:       24000,
		},
		Request: VolcengineTTSReqInfo{
			ReqID:        generateRequestID(),
			Text:         request.Input,
			Operation:    "submit",
			Model:        "",  // 🔧 豆包 TTS API 不需要 model 参数,留空避免 403 错误
			WithFrontend: 1,
			FrontendType: "unitTson",
		},
	}

	fmt.Printf("\n🔍 [ConvertAudioRequest] 默认请求结构:\n")
	fmt.Printf("  - Operation: %s\n", volcRequest.Request.Operation)
	fmt.Printf("  - WithFrontend: %d\n", volcRequest.Request.WithFrontend)
	fmt.Printf("  - FrontendType: %s\n", volcRequest.Request.FrontendType)
	fmt.Printf("  - Model: %s\n", volcRequest.Request.Model)

	if len(request.Metadata) > 0 {
		fmt.Printf("\n🔍 [ConvertAudioRequest] 开始合并 Metadata...\n")
		if err = json.Unmarshal(request.Metadata, &volcRequest); err != nil {
			fmt.Printf("❌ [ConvertAudioRequest] Metadata 解析失败: %v\n", err)
			return nil, fmt.Errorf("error unmarshalling metadata to volcengine request: %w", err)
		}
		fmt.Printf("✅ [ConvertAudioRequest] Metadata 合并成功\n")
	}

	fmt.Printf("\n🔍 [ConvertAudioRequest] 合并后的请求结构:\n")
	fmt.Printf("  - Operation: %s\n", volcRequest.Request.Operation)
	fmt.Printf("  - WithFrontend: %d\n", volcRequest.Request.WithFrontend)
	fmt.Printf("  - FrontendType: %s\n", volcRequest.Request.FrontendType)
	fmt.Printf("  - Model: %s\n", volcRequest.Request.Model)
	fmt.Printf("  - TextType: %s\n", volcRequest.Request.TextType)

	c.Set(contextKeyTTSRequest, volcRequest)

	// 根据 operation 设置流式标志
	if volcRequest.Request.Operation == "submit" {
		info.IsStream = true
		fmt.Printf("🔍 [ConvertAudioRequest] 设置为流式模式 (WebSocket)\n")
	} else {
		// query 模式或其他模式使用 HTTP 同步
		info.IsStream = false
		fmt.Printf("🔍 [ConvertAudioRequest] 设置为同步模式 (HTTP)\n")
	}

	jsonData, err := json.Marshal(volcRequest)
	if err != nil {
		return nil, fmt.Errorf("error marshalling volcengine request: %w", err)
	}

	// 🔍 调试日志:打印完整的豆包请求体
	fmt.Printf("\n" + strings.Repeat("=", 80) + "\n")
	fmt.Printf("🔍 [DEBUG] 发送给豆包的完整请求体:\n")
	fmt.Printf(strings.Repeat("=", 80) + "\n")

	// 美化打印 JSON
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, jsonData, "", "  "); err == nil {
		fmt.Println(prettyJSON.String())
	} else {
		fmt.Println(string(jsonData))
	}

	fmt.Printf(strings.Repeat("=", 80) + "\n")
	fmt.Printf("\n📋 对比参考 (tts_http_demo.py 的请求格式):\n")
	fmt.Printf(strings.Repeat("=", 80) + "\n")
	fmt.Printf(`{
  "app": {
    "appid": "7053342224",
    "token": "access_token",
    "cluster": "volcano_tts"
  },
  "user": {
    "uid": "388808087185088"
  },
  "audio": {
    "voice_type": "zh_female_meilinvyou_moon_bigtts",
    "encoding": "mp3",
    "speed_ratio": 1.0,
    "volume_ratio": 1.0,
    "pitch_ratio": 1.0,
    "rate": 24000
  },
  "request": {
    "reqid": "<uuid>",
    "text": "待合成文本",
    "text_type": "plain",
    "operation": "query",
    "with_frontend": 1,
    "frontend_type": "unitTson"
  }
}
`)
	fmt.Printf(strings.Repeat("=", 80) + "\n\n")

	fmt.Printf("🔍 关键字段对比:\n")
	fmt.Printf("  ✓ app.appid:           %s\n", volcRequest.App.AppID)
	fmt.Printf("  ✓ app.token:           %s\n", safeTruncate(volcRequest.App.Token, 20))
	fmt.Printf("  ✓ app.cluster:         %s\n", volcRequest.App.Cluster)
	fmt.Printf("  ✓ audio.voice_type:    %s\n", volcRequest.Audio.VoiceType)
	fmt.Printf("  ✓ audio.encoding:      %s\n", volcRequest.Audio.Encoding)
	fmt.Printf("  ✓ audio.speed_ratio:   %.2f\n", volcRequest.Audio.SpeedRatio)
	fmt.Printf("  ✓ audio.rate:          %d\n", volcRequest.Audio.Rate)
	fmt.Printf("  ✓ request.text:        %s\n", volcRequest.Request.Text)
	fmt.Printf("  ✓ request.operation:   %s\n", volcRequest.Request.Operation)
	fmt.Printf("  ✓ request.with_frontend: %d\n", volcRequest.Request.WithFrontend)
	fmt.Printf("  ✓ request.frontend_type: %s\n", volcRequest.Request.FrontendType)
	if volcRequest.Request.Model != "" {
		fmt.Printf("  ⚠ request.model:       %s (可能导致问题)\n", volcRequest.Request.Model)
	}
	fmt.Printf("\n")

	return bytes.NewReader(jsonData), nil
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	switch info.RelayMode {
	case constant.RelayModeImagesGenerations:
		// 🔧 修复：豆包的图生图功能使用 /api/v3/images/generations endpoint
		// 通过 image 参数区分文生图/图生图，但 dto.ImageRequest.MarshalJSON() 不会输出 Extra 字段
		// 导致 image、sequential_image_generation 等参数丢失，豆包无法识别图生图请求
		// 解决方案：手动构建包含 Extra 字段的 map

		// 创建结果 map
		result := make(map[string]interface{})

		// 1. 序列化标准字段
		baseJSON, err := json.Marshal(request)
		if err != nil {
			return nil, fmt.Errorf("marshal base request failed: %w", err)
		}
		if err := json.Unmarshal(baseJSON, &result); err != nil {
			return nil, fmt.Errorf("unmarshal to map failed: %w", err)
		}

		// 2. 合并 Extra 字段（豆包特有参数，如 image、sequential_image_generation 等）
		for k, v := range request.Extra {
			var value interface{}
			if err := json.Unmarshal(v, &value); err != nil {
				return nil, fmt.Errorf("unmarshal extra field %s failed: %w", k, err)
			}
			result[k] = value
		}

		return result, nil
	case constant.RelayModeImagesEdits:

		var requestBody bytes.Buffer
		writer := multipart.NewWriter(&requestBody)

		writer.WriteField("model", request.Model)

		formData := c.Request.PostForm
		for key, values := range formData {
			if key == "model" {
				continue
			}
			for _, value := range values {
				writer.WriteField(key, value)
			}
		}

		if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
			return nil, errors.New("failed to parse multipart form")
		}

		if c.Request.MultipartForm != nil && c.Request.MultipartForm.File != nil {
			var imageFiles []*multipart.FileHeader
			var exists bool

			if imageFiles, exists = c.Request.MultipartForm.File["image"]; !exists || len(imageFiles) == 0 {
				if imageFiles, exists = c.Request.MultipartForm.File["image[]"]; !exists || len(imageFiles) == 0 {
					foundArrayImages := false
					for fieldName, files := range c.Request.MultipartForm.File {
						if strings.HasPrefix(fieldName, "image[") && len(files) > 0 {
							foundArrayImages = true
							for _, file := range files {
								imageFiles = append(imageFiles, file)
							}
						}
					}

					if !foundArrayImages && (len(imageFiles) == 0) {
						return nil, errors.New("image is required")
					}
				}
			}

			for i, fileHeader := range imageFiles {
				file, err := fileHeader.Open()
				if err != nil {
					return nil, fmt.Errorf("failed to open image file %d: %w", i, err)
				}
				defer file.Close()

				fieldName := "image"
				if len(imageFiles) > 1 {
					fieldName = "image[]"
				}

				mimeType := detectImageMimeType(fileHeader.Filename)

				h := make(textproto.MIMEHeader)
				h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, fileHeader.Filename))
				h.Set("Content-Type", mimeType)

				part, err := writer.CreatePart(h)
				if err != nil {
					return nil, fmt.Errorf("create form part failed for image %d: %w", i, err)
				}

				if _, err := io.Copy(part, file); err != nil {
					return nil, fmt.Errorf("copy file failed for image %d: %w", i, err)
				}
			}

			if maskFiles, exists := c.Request.MultipartForm.File["mask"]; exists && len(maskFiles) > 0 {
				maskFile, err := maskFiles[0].Open()
				if err != nil {
					return nil, errors.New("failed to open mask file")
				}
				defer maskFile.Close()

				mimeType := detectImageMimeType(maskFiles[0].Filename)

				h := make(textproto.MIMEHeader)
				h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="mask"; filename="%s"`, maskFiles[0].Filename))
				h.Set("Content-Type", mimeType)

				maskPart, err := writer.CreatePart(h)
				if err != nil {
					return nil, errors.New("create form file failed for mask")
				}

				if _, err := io.Copy(maskPart, maskFile); err != nil {
					return nil, errors.New("copy mask file failed")
				}
			}
		} else {
			return nil, errors.New("no multipart form data found")
		}

		writer.Close()
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		return bytes.NewReader(requestBody.Bytes()), nil
		return request, nil
	// 根据官方文档,并没有发现豆包生图支持表单请求:https://www.volcengine.com/docs/82379/1824121
	//case constant.RelayModeImagesEdits:
	//
	//	var requestBody bytes.Buffer
	//	writer := multipart.NewWriter(&requestBody)
	//
	//	writer.WriteField("model", request.Model)
	//
	//	formData := c.Request.PostForm
	//	for key, values := range formData {
	//		if key == "model" {
	//			continue
	//		}
	//		for _, value := range values {
	//			writer.WriteField(key, value)
	//		}
	//	}
	//
	//	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
	//		return nil, errors.New("failed to parse multipart form")
	//	}
	//
	//	if c.Request.MultipartForm != nil && c.Request.MultipartForm.File != nil {
	//		var imageFiles []*multipart.FileHeader
	//		var exists bool
	//
	//		if imageFiles, exists = c.Request.MultipartForm.File["image"]; !exists || len(imageFiles) == 0 {
	//			if imageFiles, exists = c.Request.MultipartForm.File["image[]"]; !exists || len(imageFiles) == 0 {
	//				foundArrayImages := false
	//				for fieldName, files := range c.Request.MultipartForm.File {
	//					if strings.HasPrefix(fieldName, "image[") && len(files) > 0 {
	//						foundArrayImages = true
	//						for _, file := range files {
	//							imageFiles = append(imageFiles, file)
	//						}
	//					}
	//				}
	//
	//				if !foundArrayImages && (len(imageFiles) == 0) {
	//					return nil, errors.New("image is required")
	//				}
	//			}
	//		}
	//
	//		for i, fileHeader := range imageFiles {
	//			file, err := fileHeader.Open()
	//			if err != nil {
	//				return nil, fmt.Errorf("failed to open image file %d: %w", i, err)
	//			}
	//			defer file.Close()
	//
	//			fieldName := "image"
	//			if len(imageFiles) > 1 {
	//				fieldName = "image[]"
	//			}
	//
	//			mimeType := detectImageMimeType(fileHeader.Filename)
	//
	//			h := make(textproto.MIMEHeader)
	//			h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, fileHeader.Filename))
	//			h.Set("Content-Type", mimeType)
	//
	//			part, err := writer.CreatePart(h)
	//			if err != nil {
	//				return nil, fmt.Errorf("create form part failed for image %d: %w", i, err)
	//			}
	//
	//			if _, err := io.Copy(part, file); err != nil {
	//				return nil, fmt.Errorf("copy file failed for image %d: %w", i, err)
	//			}
	//		}
	//
	//		if maskFiles, exists := c.Request.MultipartForm.File["mask"]; exists && len(maskFiles) > 0 {
	//			maskFile, err := maskFiles[0].Open()
	//			if err != nil {
	//				return nil, errors.New("failed to open mask file")
	//			}
	//			defer maskFile.Close()
	//
	//			mimeType := detectImageMimeType(maskFiles[0].Filename)
	//
	//			h := make(textproto.MIMEHeader)
	//			h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="mask"; filename="%s"`, maskFiles[0].Filename))
	//			h.Set("Content-Type", mimeType)
	//
	//			maskPart, err := writer.CreatePart(h)
	//			if err != nil {
	//				return nil, errors.New("create form file failed for mask")
	//			}
	//
	//			if _, err := io.Copy(maskPart, maskFile); err != nil {
	//				return nil, errors.New("copy mask file failed")
	//			}
	//		}
	//	} else {
	//		return nil, errors.New("no multipart form data found")
	//	}
	//
	//	writer.Close()
	//	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	//	return bytes.NewReader(requestBody.Bytes()), nil

	default:
		return request, nil
	}
}

func detectImageMimeType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		if strings.HasPrefix(ext, ".jp") {
			return "image/jpeg"
		}
		return "image/png"
	}
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	fmt.Printf("\n🔍 [GetRequestURL] 开始构建请求URL\n")
	fmt.Printf("🔍 [GetRequestURL] RelayMode: %d\n", info.RelayMode)
	fmt.Printf("🔍 [GetRequestURL] IsStream: %v\n", info.IsStream)

	baseUrl := info.ChannelBaseUrl
	if baseUrl == "" {
		baseUrl = channelconstant.ChannelBaseURLs[channelconstant.ChannelTypeVolcEngine]
	}
	fmt.Printf("🔍 [GetRequestURL] BaseURL: %s\n", baseUrl)

	switch info.RelayFormat {
	case types.RelayFormatClaude:
		if strings.HasPrefix(info.UpstreamModelName, "bot") {
			return fmt.Sprintf("%s/api/v3/bots/chat/completions", baseUrl), nil
		}
		return fmt.Sprintf("%s/api/v3/chat/completions", baseUrl), nil
	default:
		switch info.RelayMode {
		case constant.RelayModeChatCompletions:
			if strings.HasPrefix(info.UpstreamModelName, "bot") {
				return fmt.Sprintf("%s/api/v3/bots/chat/completions", baseUrl), nil
			}
			return fmt.Sprintf("%s/api/v3/chat/completions", baseUrl), nil
		case constant.RelayModeEmbeddings:
			return fmt.Sprintf("%s/api/v3/embeddings", baseUrl), nil
		//豆包的图生图也走generations接口: https://www.volcengine.com/docs/82379/1824121
		case constant.RelayModeImagesGenerations, constant.RelayModeImagesEdits:
			return fmt.Sprintf("%s/api/v3/images/generations", baseUrl), nil
		//case constant.RelayModeImagesEdits:
		//	return fmt.Sprintf("%s/api/v3/images/edits", baseUrl), nil
		case constant.RelayModeRerank:
			return fmt.Sprintf("%s/api/v3/rerank", baseUrl), nil
		case constant.RelayModeAudioSpeech:
			// 根据 IsStream 标志决定使用 WebSocket 还是 HTTP
			if baseUrl == channelconstant.ChannelBaseURLs[channelconstant.ChannelTypeVolcEngine] {
				if info.IsStream {
					url := "wss://openspeech.bytedance.com/api/v1/tts/ws_binary"
					fmt.Printf("🔍 [GetRequestURL] 返回 WebSocket URL: %s\n", url)
					return url, nil
				}
				// HTTP 同步模式 (operation=query)
				url := "https://openspeech.bytedance.com/api/v1/tts"
				fmt.Printf("🔍 [GetRequestURL] 返回 HTTP URL: %s\n", url)
				return url, nil
			}
			customUrl := fmt.Sprintf("%s/v1/audio/speech", baseUrl)
			fmt.Printf("🔍 [GetRequestURL] 返回自定义 URL: %s\n", customUrl)
			return customUrl, nil
		default:
		}
	}
	return "", fmt.Errorf("unsupported relay mode: %d", info.RelayMode)
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)

	if info.RelayMode == constant.RelayModeAudioSpeech {
		parts := strings.Split(info.ApiKey, "|")
		if len(parts) == 2 {
			req.Set("Authorization", "Bearer;"+parts[1])
		}
		req.Set("Content-Type", "application/json")
		return nil
	} else if info.RelayMode == constant.RelayModeImagesEdits {
		req.Set("Content-Type", gin.MIMEJSON)
	}

	req.Set("Authorization", "Bearer "+info.ApiKey)
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}

	if strings.HasSuffix(info.UpstreamModelName, "-thinking") && strings.HasPrefix(info.UpstreamModelName, "deepseek") {
		info.UpstreamModelName = strings.TrimSuffix(info.UpstreamModelName, "-thinking")
		request.Model = info.UpstreamModelName
		request.THINKING = json.RawMessage(`{"type": "enabled"}`)
	}
	return request, nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	fmt.Printf("\n🔍 [DoRequest] 开始发送请求\n")
	fmt.Printf("🔍 [DoRequest] RelayMode: %d\n", info.RelayMode)

	if info.RelayMode == constant.RelayModeAudioSpeech {
		baseUrl := info.ChannelBaseUrl
		if baseUrl == "" {
			baseUrl = channelconstant.ChannelBaseURLs[channelconstant.ChannelTypeVolcEngine]
		}

		fmt.Printf("🔍 [DoRequest] BaseURL: %s\n", baseUrl)
		fmt.Printf("🔍 [DoRequest] IsStream: %v\n", info.IsStream)

		if baseUrl == channelconstant.ChannelBaseURLs[channelconstant.ChannelTypeVolcEngine] {
			if info.IsStream {
				fmt.Printf("🔍 [DoRequest] WebSocket 流式模式,返回 nil (由 DoResponse 处理)\n")
				return nil, nil
			}
		}
	}

	fmt.Printf("🔍 [DoRequest] 执行标准 HTTP 请求\n")
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	fmt.Printf("\n🔍 [DoResponse] 开始处理响应\n")
	fmt.Printf("🔍 [DoResponse] RelayMode: %d\n", info.RelayMode)

	if info.RelayMode == constant.RelayModeAudioSpeech {
		encoding := mapEncoding(c.GetString(contextKeyResponseFormat))
		fmt.Printf("🔍 [DoResponse] Audio Encoding: %s\n", encoding)
		fmt.Printf("🔍 [DoResponse] IsStream: %v\n", info.IsStream)

		if info.IsStream {
			fmt.Printf("🔍 [DoResponse] 处理 WebSocket 流式响应\n")
			volcRequestInterface, exists := c.Get(contextKeyTTSRequest)
			if !exists {
				return nil, types.NewErrorWithStatusCode(
					errors.New("volcengine TTS request not found in context"),
					types.ErrorCodeBadRequestBody,
					http.StatusInternalServerError,
				)
			}

			volcRequest, ok := volcRequestInterface.(VolcengineTTSRequest)
			if !ok {
				return nil, types.NewErrorWithStatusCode(
					errors.New("invalid volcengine TTS request type"),
					types.ErrorCodeBadRequestBody,
					http.StatusInternalServerError,
				)
			}

			// Get the WebSocket URL
			requestURL, urlErr := a.GetRequestURL(info)
			if urlErr != nil {
				return nil, types.NewErrorWithStatusCode(
					urlErr,
					types.ErrorCodeBadRequestBody,
					http.StatusInternalServerError,
				)
			}
			return handleTTSWebSocketResponse(c, requestURL, volcRequest, info, encoding)
		}
		fmt.Printf("🔍 [DoResponse] 处理 HTTP 同步响应\n")
		if resp != nil {
			fmt.Printf("🔍 [DoResponse] HTTP Status: %d\n", resp.StatusCode)
		}
		return handleTTSResponse(c, resp, info, encoding)
	}

	fmt.Printf("🔍 [DoResponse] 使用 OpenAI 适配器处理响应\n")
	adaptor := openai.Adaptor{}
	usage, err = adaptor.DoResponse(c, resp, info)
	return
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
