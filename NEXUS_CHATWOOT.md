# Integração Nativa: Nexus ↔ Chatwoot

Este documento registra as implementações da integração nativa e direta entre o core da Evolution API (Evolution GO) e o Chatwoot, eliminando a necessidade de ferramentas intermediárias de automação (como n8n).

## 🚀 Fases Concluídas (1 a 4) - Roteamento de Entrada (Inbound)

### 1. Arquitetura de Banco de Dados (`models.go`)

- Criação automática da tabela `chatwoot_configs` no PostgreSQL utilizando o GORM.
- Estruturação para armazenar de forma segura: Nome da Instância, Account ID, Token do Chatwoot, URL e o ID da Inbox gerada.

### 2. API de Configuração (`routes.go` e `controller.go`)

- Rota dedicada `POST /chatwoot/set/:instance` para injetar credenciais via requisição HTTP.
- Lógica de **Upsert**: Proteção contra duplicidade de caixas de entrada. Se a instância já existir, o sistema apenas atualiza os dados, prevenindo a recriação de Inboxes no Chatwoot.

### 3. Orquestração do Chatwoot (`client.go`)

- **HttpClient Customizado**: Implementação de reuso de conexões com `Timeout` de 10 segundos, prevenindo Memory Leaks e travamentos no core da Evolution.
- **Automação de Inbox**: Criação automática da "Caixa de Entrada" (Canal API) no painel do Chatwoot assim que a instância é configurada.
- **Gestão Inteligente de Conversas (Prevenção do Erro 422)**:
  - Sistema verifica a existência prévia de contatos via `Search` (Telefone/Identificador).
  - Consulta ativa de status de conversas: O sistema reaproveita conversas ativas. Se a conversa foi marcada como "Resolvida" (`resolved`) pelo atendente, uma nova aba de atendimento é gerada automaticamente.

### 4. Interceptação Core (`whatsmeow.go`)

- Gancho assíncrono (Goroutines) implementado diretamente no motor de recebimento de mensagens do WhatsMeow, garantindo latência zero.
- **Tratamento Avançado de Grupos**:
  - Identificação de grupos vs. DMs (`IsGroup`).
  - Resgate nativo do Nome Oficial do Grupo e injeção do prefixo visual (📢).
  - Resgate do nome/telefone do participante que enviou a mensagem, formatando a entrega no painel do agente como: `✋ *Número / Nome*: Texto`.

---

_Status Atual: Recepção de textos 100% funcional para DMs e Grupos._
