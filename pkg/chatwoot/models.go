package chatwoot

import "gorm.io/gorm"

// ChatwootConfig possui os campos necessários para virar uma tabela no PostgreSQL
type ChatwootConfig struct {
	gorm.Model          // Adiciona automaticamente ID, CreatedAt, UpdatedAt e DeletedAt
	InstanceName string `gorm:"uniqueIndex;not null" json:"instance_name"` // Nome da instância
	AccountID    string `json:"account_id"`
	Token        string `json:"token"`
	BaseURL      string `json:"base_url"`
	SignMsg      bool   `json:"sign_msg"`
	InboxID      int    `json:"inbox_id"` // Será preenchido quando o Chatwoot confirmar a criação da caixa
}

// ChatwootMessage é o "molde" do JSON que vamos enviar para a API do Chatwoot
// quando um cliente mandar mensagem no WhatsApp.
type ChatwootMessage struct {
	Content     string `json:"content"`
	MessageType string `json:"message_type"` // "incoming" (recebida do cliente) ou "outgoing" (enviada pelo agente)
}

// ContactCreatePayload é o molde para criar o contato do cliente lá no Chatwoot
type ContactCreatePayload struct {
	InboxID     int    `json:"inbox_id"`
	Name        string `json:"name"`
	PhoneNumber string `json:"phone_number"`
}

// ChatwootWebhookPayload representa o JSON que o Chatwoot nos envia
type ChatwootWebhookPayload struct {
	Event       string `json:"event"`
	MessageType string `json:"message_type"`
	Private     bool   `json:"private"`
	Content     string `json:"content"`
	Inbox       struct {
		Name string `json:"name"`
	} `json:"inbox"`
	Conversation struct {
		Meta struct {
			Sender struct {
				PhoneNumber string `json:"phone_number"`
				Identifier  string `json:"identifier"`
			} `json:"sender"`
		} `json:"meta"`
	} `json:"conversation"`
}
