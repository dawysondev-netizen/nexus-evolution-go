package chatwoot

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GlobalDB guarda a conexão do banco para usarmos na interceptação
var GlobalDB *gorm.DB

func RegisterRoutes(router *gin.Engine, db *gorm.DB) {
	GlobalDB = db // Salva o banco de dados na variável global do módulo!

	chatwootGroup := router.Group("/chatwoot")
	{
		chatwootGroup.POST("/set/:instance", func(c *gin.Context) {
			SetChatwootConfig(c, db)
		})
	}
}
