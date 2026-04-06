package chatwoot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto" // Essencial para ativar a pré-visualização de mídias
	"net/url"
	"strings"
	"time"
)

// Otimizado para 30s para suportar o upload de documentos, áudios e vídeos mais pesados
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

func CreateInbox(config *ChatwootConfig) error {
	baseURL := strings.TrimRight(config.BaseURL, "/")
	url := fmt.Sprintf("%s/api/v1/accounts/%s/inboxes", baseURL, config.AccountID)

	payload := map[string]interface{}{
		"name": config.InstanceName,
		"channel": map[string]interface{}{
			"type":        "api",
			"webhook_url": "",
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("erro ao converter payload para JSON: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("erro ao montar requisição: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api_access_token", config.Token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("erro na requisição HTTP: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chatwoot retornou status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("erro ao decodificar resposta JSON: %v", err)
	}

	config.InboxID = result.ID
	return nil
}

// ATUALIZADO: Inclui o parâmetro mimeType para renderizar o arquivo corretamente
func ProcessIncomingMessage(instanceName string, contactName string, contactNumber string, messageText string, fileData []byte, fileName string, mimeType string) {
	log.Printf("[Chatwoot DEBUG] 1. Processando... Instância: '%s', Contato: '%s', Número: '%s'", instanceName, contactName, contactNumber)

	if GlobalDB == nil {
		log.Println("[Chatwoot ERRO] PARADA: GlobalDB está nulo!")
		return
	}

	var config ChatwootConfig
	if err := GlobalDB.Where("instance_name = ?", instanceName).First(&config).Error; err != nil {
		return
	}

	if config.InboxID == 0 {
		return
	}

	contactID, err := createOrGetContact(&config, contactName, contactNumber)
	if err != nil {
		log.Printf("[Chatwoot ERRO] Erro ao criar contato: %v\n", err)
		return
	}

	conversationID, err := createOrGetConversation(&config, contactID)
	if err != nil {
		log.Printf("[Chatwoot ERRO] Erro ao criar/buscar conversa: %v\n", err)
		return
	}

	// ROTEAMENTO: Se tiver arquivo (len > 0), dispara a função de mídia. Senão, dispara texto.
	if len(fileData) > 0 {
		sendMediaToChatwoot(&config, conversationID, messageText, fileData, fileName, mimeType)
	} else {
		sendTextToChatwoot(&config, conversationID, messageText)
	}
}

func createOrGetContact(config *ChatwootConfig, name string, phone string) (int, error) {
	baseURL := strings.TrimRight(config.BaseURL, "/")
	cleanPhone := strings.ReplaceAll(phone, "@s.whatsapp.net", "")
	cleanPhone = strings.ReplaceAll(cleanPhone, "@g.us", "")

	apiUrl := fmt.Sprintf("%s/api/v1/accounts/%s/contacts", baseURL, config.AccountID)

	payload := map[string]interface{}{
		"inbox_id": config.InboxID,
		"name":     name,
	}

	if len(cleanPhone) > 15 {
		payload["identifier"] = cleanPhone
	} else {
		payload["phone_number"] = "+" + cleanPhone
	}

	jsonData, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", apiUrl, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api_access_token", config.Token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 422 {
		searchQuery := cleanPhone
		if len(cleanPhone) <= 15 {
			searchQuery = "+" + cleanPhone
		}

		encodedQuery := url.QueryEscape(searchQuery)
		searchURL := fmt.Sprintf("%s/api/v1/accounts/%s/contacts/search?q=%s", baseURL, config.AccountID, encodedQuery)

		reqSearch, _ := http.NewRequest("GET", searchURL, nil)
		reqSearch.Header.Set("api_access_token", config.Token)

		respSearch, errSearch := httpClient.Do(reqSearch)
		if errSearch == nil {
			defer respSearch.Body.Close() // Otimização: Garante fechamento do body da busca
			var searchResult struct {
				Payload []struct {
					ID int `json:"id"`
				} `json:"payload"`
			}
			if err := json.NewDecoder(respSearch.Body).Decode(&searchResult); err == nil {
				if len(searchResult.Payload) > 0 {
					return searchResult.Payload[0].ID, nil
				}
			}
		}
	}

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("status HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Payload struct {
			Contact struct {
				ID int `json:"id"`
			} `json:"contact"`
		} `json:"payload"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	return result.Payload.Contact.ID, nil
}

func createOrGetConversation(config *ChatwootConfig, contactID int) (int, error) {
	baseURL := strings.TrimRight(config.BaseURL, "/")

	getURL := fmt.Sprintf("%s/api/v1/accounts/%s/contacts/%d/conversations", baseURL, config.AccountID, contactID)
	reqGet, _ := http.NewRequest("GET", getURL, nil)
	reqGet.Header.Set("api_access_token", config.Token)

	respGet, errGet := httpClient.Do(reqGet)
	if errGet == nil {
		defer respGet.Body.Close() // Otimização: Prevenção de Memory Leak
		if respGet.StatusCode == 200 {
			var getResult struct {
				Payload []struct {
					ID      int    `json:"id"`
					InboxID int    `json:"inbox_id"`
					Status  string `json:"status"`
				} `json:"payload"`
			}
			if err := json.NewDecoder(respGet.Body).Decode(&getResult); err == nil {
				for _, conv := range getResult.Payload {
					if conv.InboxID == config.InboxID && conv.Status != "resolved" {
						return conv.ID, nil
					}
				}
			}
		}
	}

	url := fmt.Sprintf("%s/api/v1/accounts/%s/conversations", baseURL, config.AccountID)
	payload := map[string]interface{}{
		"inbox_id":   config.InboxID,
		"contact_id": contactID,
	}

	jsonData, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api_access_token", config.Token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("status HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	return result.ID, nil
}

func sendTextToChatwoot(config *ChatwootConfig, conversationID int, text string) {
	baseURL := strings.TrimRight(config.BaseURL, "/")
	url := fmt.Sprintf("%s/api/v1/accounts/%s/conversations/%d/messages", baseURL, config.AccountID, conversationID)

	payload := map[string]interface{}{
		"content":      text,
		"message_type": "incoming",
	}

	jsonData, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api_access_token", config.Token)

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[Chatwoot] Erro ao enviar mensagem HTTP: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("[Chatwoot] Erro %d na API ao enviar texto: %s", resp.StatusCode, string(bodyBytes))
	} else {
		log.Println("[Chatwoot DEBUG] Mensagem de texto injetada no Chatwoot com sucesso!")
	}
}

// NOVA FUNÇÃO: Upload nativo com Cabeçalho MIME para ativar Preview no Chatwoot
func sendMediaToChatwoot(config *ChatwootConfig, conversationID int, caption string, fileData []byte, fileName string, mimeType string) {
	baseURL := strings.TrimRight(config.BaseURL, "/")
	apiUrl := fmt.Sprintf("%s/api/v1/accounts/%s/conversations/%d/messages", baseURL, config.AccountID, conversationID)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	if caption == "" {
		caption = " "
	}

	_ = writer.WriteField("content", caption)
	_ = writer.WriteField("message_type", "incoming")

	// Otimização: Forçando o Chatwoot a reconhecer o formato correto da mídia
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="attachments[]"; filename="%s"`, fileName))
	h.Set("Content-Type", mimeType)

	part, err := writer.CreatePart(h)
	if err == nil {
		_, _ = io.Copy(part, bytes.NewReader(fileData))
	}
	_ = writer.Close()

	req, _ := http.NewRequest("POST", apiUrl, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("api_access_token", config.Token)

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[Chatwoot ERRO] Falha na rede ao enviar mídia: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("[Chatwoot ERRO] Erro %d da API ao enviar Mídia: %s", resp.StatusCode, string(bodyBytes))
	} else {
		log.Printf("[Chatwoot DEBUG] Mídia (%s | %s) enviada com sucesso para conversa %d!", fileName, mimeType, conversationID)
	}
}
