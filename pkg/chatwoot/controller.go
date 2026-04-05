package chatwoot

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

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
