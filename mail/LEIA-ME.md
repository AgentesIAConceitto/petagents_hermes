# mail/ — o lado Outlook do PetAgent (fora do binário, de propósito)

Os scripts que leem o Outlook **não** rodam dentro do PetAgent. O app em Go nunca
fala COM: ele dispara o `poll_silencioso.vbs`, que sobe o `poll.ps1` **oculto e
não-elevado** (o COM do Outlook recusa processo elevado), e depois lê o JSON.

```
app (Go, email.go)
  └─ explorer.exe  →  poll_silencioso.vbs  →  powershell -File poll.ps1   (não-elevado, sem janela)
  └─ lê C:\petagent_mail\inbox.json   (cabeçalhos)  e  status.json  (o que aconteceu)
```

## Por que aqui e não em C:\petagent_mail

Esta pasta é a **cópia versionada**. A cópia que roda de verdade é
`C:\petagent_mail\` (máquina do dono). Quando editar, **edite os dois** — e
mantenha idênticos:

```bash
cp mail/poll.ps1 mail/poll_silencioso.vbs /c/petagent_mail/
```

Motivo de existir: em 04/10/2026 um analista externo procurou o "código do
PetAgent" para investigar um incidente de recursos do Outlook, **não achou nada** e
concluiu que só existia o executável — porque a automação toda estava fora do
repositório, em `C:\petagent_mail\`. Custou uma análise com premissas erradas.

## As cinco regras do poll (todas pagas no incidente de 04/10/2026)

O Outlook parou de abrir com erro de falta de recursos. Causa: a tabela de memória
compartilhada do MAPI transbordou — ~364 consultas em 3 dias, cada uma criando até
~100 objetos COM **sem soltar**, somadas a uma sessão do Word aberta desde 27/09.

1. **ATACHAR, NUNCA SUBIR.** `New-Object -ComObject Outlook.Application` **sobe** o
   Outlook quando ele está fechado, e a instância fica viva depois que o script sai.
   Medido: `10:29:49` disparo → `10:29:52` evento de início do Outlook →
   `10:29:54` falha `0x90740111`. Agora: `GetActiveObject`; sem Outlook aberto, o
   poll **adia** e sai sem iniciar nada.
2. **UMA COLETA POR VEZ.** Mutex `Global\PetAgentOutlookPoll`. Automático, manual e
   de boot nunca rodam juntos (era possível: o boot dispara um poll imediato).
3. **SOLTAR TODO OBJETO COM, em ordem inversa, em `finally`** — e cada `MailItem`
   logo depois de lido. `MailItem` → `Items`/`Restrict` → `MAPIFolder` → `Stores` →
   `Namespace` → `Application`, mais `GC.Collect`/`WaitForPendingFinalizers`.
4. **ACESSO INDEXADO** (`.Item($i)`), não `foreach` sobre coleção COM.
5. **STATUS EXPLÍCITO** (`ok` | `sem_outlook` | `ocupado` | `erro`) em `status.json`,
   para o app parar de tratar leitura velha como nova (ele tinha um `Sleep(40s)` cego).

## Detalhes que já custaram tempo

- O PowerShell grava UTF-8 **com BOM** por padrão; o Go falha no JSON com BOM.
  Por isso `WriteAllText` com `UTF8Encoding($false)`.
- `GetActiveObject` falha **também** quando o chamador está elevado (não só quando o
  Outlook está fechado) — o script agora diferencia os dois casos e marca `erro`
  quando está elevado, em vez de adiar em silêncio.
- `$inbox.UnReadItemCount` é do **store escolhido**; a conta certa vem de `conta.txt`
  (`SUA_CONTA@EXEMPLO.COM`), nunca de `conta-pessoal@exemplo.com`.
- O `status.json` é por tentativa, e o app espera o `quando` **avançar** em vez de
  dormir um tempo fixo.
