package chatwoot

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GlobalDB guarda a conexão do banco para usarmos na interceptação assíncrona
var GlobalDB *gorm.DB

// RegisterRoutes inicializa os endpoints da API para o módulo do Chatwoot
func RegisterRoutes(router *gin.Engine, db *gorm.DB) {
	GlobalDB = db // Salva o banco de dados na variável global do módulo

	chatwootGroup := router.Group("/chatwoot")
	{
		// Rota para configurar ou atualizar a conexão de uma instância com o Chatwoot
		chatwootGroup.POST("/set/:instance", func(c *gin.Context) {
			SetChatwootConfig(c, db)
		})

		// Rota para receber os Webhooks disparados pelo Chatwoot (Respostas do Atendente)
		chatwootGroup.POST("/webhook", func(c *gin.Context) {
			ChatwootWebhook(c, db)
		})
	}
}
