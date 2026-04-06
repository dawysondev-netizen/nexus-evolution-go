package chatwoot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// localClient otimiza as chamadas internas para a EvoGO reaproveitando conexões TCP
var localClient = &http.Client{
	Timeout: 10 * time.Second,
}

func SetChatwootConfig(c *gin.Context, db *gorm.DB) {
	instanceName := c.Param("instance")
	var config ChatwootConfig

	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON inválido", "details": err.Error()})
		return
	}

	config.InstanceName = instanceName

	// 1. Busca se já temos dados dessa instância
	var existingConfig ChatwootConfig
	if err := db.Where("instance_name = ?", instanceName).First(&existingConfig).Error; err == nil {
		// Se já existir, pega o ID e o InboxID para não duplicar e não criar duas caixas
		config.ID = existingConfig.ID
		config.InboxID = existingConfig.InboxID
	}

	// 2. Se o InboxID for 0 (caixa ainda não criada), mandamos o comando para o Chatwoot!
	if config.InboxID == 0 {
		log.Printf("[Chatwoot] Solicitando criação da Inbox '%s' via API...", instanceName)

		err := CreateInbox(&config)
		if err != nil {
			log.Printf("[Chatwoot] Erro ao criar Inbox: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Falha ao comunicar com o Chatwoot para criar a Inbox",
				"details": err.Error(),
			})
			return
		}
		log.Printf("[Chatwoot] Inbox criada com sucesso! ID no Chatwoot: %d", config.InboxID)
	}

	// 3. Salva os dados no banco de dados da EvoGO (agora incluindo o InboxID gerado)
	if err := db.Save(&config).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Falha ao salvar no banco", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Instância conectada e Inbox gerada no Chatwoot",
		"data":    config,
	})
}

// ChatwootWebhook recebe as mensagens do painel do Chatwoot e envia pro WhatsApp
func ChatwootWebhook(c *gin.Context, db *gorm.DB) {
	var payload ChatwootWebhookPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusOK, gin.H{"status": "ignored", "reason": "invalid_json"})
		return
	}

	// Queremos apenas mensagens CRIADAS pelo ATENDENTE (outgoing) e que NÃO sejam notas privadas (private)
	if payload.Event != "message_created" || payload.Private || payload.MessageType != "outgoing" {
		c.JSON(http.StatusOK, gin.H{"status": "ignored", "reason": "not_outgoing_message"})
		return
	}

	instanceName := payload.Inbox.Name
	if instanceName == "" {
		c.JSON(http.StatusOK, gin.H{"status": "ignored", "reason": "empty_inbox"})
		return
	}

	// Prioriza o Identifier (Usado para Grupos), senão usa o PhoneNumber
	number := payload.Conversation.Meta.Sender.Identifier
	if number == "" {
		number = payload.Conversation.Meta.Sender.PhoneNumber
	}

	// Limpa o número (Remove +, espaços, etc)
	number = strings.ReplaceAll(number, "+", "")
	number = strings.TrimSpace(number)

	if number == "" || payload.Content == "" {
		c.JSON(http.StatusOK, gin.H{"status": "ignored", "reason": "empty_number_or_content"})
		return
	}

	log.Printf("[Chatwoot Webhook] Disparando mensagem para %s via Instância '%s'...", number, instanceName)

	// Busca o token da instância no nosso banco de dados
	var config ChatwootConfig
	if err := db.Where("instance_name = ?", instanceName).First(&config).Error; err != nil {
		log.Printf("[Chatwoot Webhook] Instância '%s' não configurada no banco.", instanceName)
		c.JSON(http.StatusOK, gin.H{"status": "ignored", "reason": "instance_not_configured"})
		return
	}

	// Dispara a mensagem usando a API local da Evolution
	err := sendTextToEvolutionAPI(instanceName, config.Token, number, payload.Content)
	if err != nil {
		log.Printf("[Chatwoot Webhook] ERRO ao enviar para Evolution: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[Chatwoot Webhook] SUCESSO! Mensagem enviada para %s", number)
	c.JSON(http.StatusOK, gin.H{"status": "success"})
}

// sendTextToEvolutionAPI faz uma requisição HTTP interna (localhost) para a Evo GO disparar a mensagem
func sendTextToEvolutionAPI(instanceName string, token string, number string, text string) error {
	url := "http://localhost:8080/send/text"

	payload := map[string]interface{}{
		"number": number,
		"text":   text,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("erro no json marshal: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("erro na requisicao: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", token)
	req.Header.Set("instance", instanceName)

	// Utiliza o cliente global otimizado em vez de criar um novo
	resp, err := localClient.Do(req)
	if err != nil {
		return fmt.Errorf("falha de conexao local: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}
