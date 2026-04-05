package chatwoot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
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

func ProcessIncomingMessage(instanceName string, contactName string, contactNumber string, messageText string) {
	log.Printf("[Chatwoot DEBUG] 1. Iniciando processamento... Instância: '%s', Contato: '%s', Número: '%s'", instanceName, contactName, contactNumber)

	if messageText == "" {
		log.Println("[Chatwoot DEBUG] 2. PARADA: O texto da mensagem está vazio!")
		return
	}

	if GlobalDB == nil {
		log.Println("[Chatwoot ERRO] 3. PARADA: GlobalDB está nulo!")
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
		log.Printf("[Chatwoot ERRO] 5. Erro ao criar contato: %v\n", err)
		return
	}

	// NÃO PRECISAMOS MAIS DO sourceID AQUI
	conversationID, err := createOrGetConversation(&config, contactID)
	if err != nil {
		log.Printf("[Chatwoot ERRO] 6. Erro ao criar/buscar conversa: %v\n", err)
		return
	}

	sendMessageToChatwoot(&config, conversationID, messageText)
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
		log.Printf("[Chatwoot DEBUG] Contato já existe. Buscando ID via Search...")

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
			defer respSearch.Body.Close()
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

// LOGICA NOVA: Busca conversa antes de criar (Ignorando Resolvidas)
func createOrGetConversation(config *ChatwootConfig, contactID int) (int, error) {
	baseURL := strings.TrimRight(config.BaseURL, "/")

	// 1. TENTA BUSCAR UMA CONVERSA EXISTENTE ATIVA NESTE INBOX
	getURL := fmt.Sprintf("%s/api/v1/accounts/%s/contacts/%d/conversations", baseURL, config.AccountID, contactID)
	reqGet, _ := http.NewRequest("GET", getURL, nil)
	reqGet.Header.Set("api_access_token", config.Token)

	respGet, errGet := httpClient.Do(reqGet)
	if errGet == nil && respGet.StatusCode == 200 {
		defer respGet.Body.Close()
		var getResult struct {
			Payload []struct {
				ID      int    `json:"id"`
				InboxID int    `json:"inbox_id"`
				Status  string `json:"status"` // NOVO: Lemos o status da conversa!
			} `json:"payload"`
		}
		if err := json.NewDecoder(respGet.Body).Decode(&getResult); err == nil {
			for _, conv := range getResult.Payload {
				// Se for desta Inbox e NÃO estiver Resolvida, reaproveitamos a aba!
				if conv.InboxID == config.InboxID && conv.Status != "resolved" {
					log.Printf("[Chatwoot DEBUG] Aproveitando conversa ativa (Status: %s): ID %d", conv.Status, conv.ID)
					return conv.ID, nil
				}
			}
		}
	} else if respGet != nil {
		respGet.Body.Close()
	}

	// 2. SE NÃO ACHOU NENHUMA (OU TODAS ESTAVAM RESOLVIDAS), CRIA UMA NOVA!
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

	log.Printf("[Chatwoot DEBUG] Nova conversa criada: ID %d", result.ID)
	return result.ID, nil
}

func sendMessageToChatwoot(config *ChatwootConfig, conversationID int, text string) {
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
		log.Printf("[Chatwoot] Erro %d na API ao enviar mensagem: %s", resp.StatusCode, string(bodyBytes))
	} else {
		log.Println("[Chatwoot DEBUG] 7. SUCESSO ABSOLUTO! Mensagem injetada no Chatwoot!")
	}
}
