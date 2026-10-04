# Le cabecalhos do Outlook — v2 (04/10/2026), depois do incidente de "falta de
# recursos" ao abrir o Outlook (esgotamento da memoria compartilhada do MAPI).
#
# ⚠️ CINCO REGRAS QUE NAO PODEM REGREDIR (todas pagas no incidente):
#
#  1. ATACHAR, NUNCA SUBIR. `New-Object -ComObject Outlook.Application` SOBE o
#     Outlook quando ele esta fechado — e a instancia fica viva depois que o
#     script sai. Medido em 04/10: 10:29:49 disparo do poll -> 10:29:52 evento de
#     inicio do Outlook -> 10:29:54 falha 0x90740111. Agora: GetActiveObject e, se
#     nao houver Outlook ABERTO, o poll ADIA (grava status e sai).
#  2. UMA COLETA POR VEZ: mutex global. Automatico + manual + boot nunca juntos.
#  3. SOLTAR TODO OBJETO COM, em ordem inversa, em finally — e cada MailItem
#     imediatamente apos ser lido. Sem isso a tabela de memoria compartilhada
#     enche e o Outlook deixa de abrir. (364 polls x ~100 objetos = entupimento.)
#  4. ACESSO INDEXADO (`.Item($i)`), nao enumerador/foreach sobre colecao COM.
#  5. STATUS EXPLICITO (ok | sem_outlook | erro | ocupado) para o app parar de
#     tratar leitura velha como leitura de agora.
#
# SOMENTE LEITURA: nao apaga, nao move, nao marca, nao responde. NUNCA chama
# Outlook.Quit() — quem so atacha nao encerra a sessao de ninguem.

$saida   = "C:\petagent_mail\inbox.json"
$statusF = "C:\petagent_mail\status.json"
$erroF   = "C:\petagent_mail\erro.txt"
$conf    = "C:\petagent_mail\conta.txt"

$conta = "SUA_CONTA@EXEMPLO.COM"
if (Test-Path $conf) { $c = (Get-Content $conf -Raw).Trim(); if ($c) { $conta = $c } }

function GravaJson($caminho, $obj) {
    [System.IO.File]::WriteAllText($caminho, ($obj | ConvertTo-Json -Depth 4),
        (New-Object System.Text.UTF8Encoding($false)))
}
function GravaStatus($estado, $n, $extra) {
    $o = [ordered]@{ estado = $estado; quando = (Get-Date).ToString("s"); mensagens = $n }
    if ($extra) { $o.detalhe = [string]$extra }
    GravaJson $statusF $o
}
function Solta($o) {
    if ($null -ne $o) { try { [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($o) } catch {} }
}

function Cabecalho($m, [ref]$pa) {
    $pa.Value = $null
    $de = ""
    try { $de = [string]$m.SenderEmailAddress } catch {}
    if ($de -like "/O=*") {   # conta Exchange devolve DN (X500) em vez de SMTP
        try {
            $pa.Value = $m.PropertyAccessor
            $de = [string]$pa.Value.GetProperty("http://schemas.microsoft.com/mapi/proptag/0x0C1F001E")
        } catch {}
    }
    $quando = ""
    try { $quando = $m.ReceivedTime.ToString("s") } catch { try { $quando = $m.SentOn.ToString("s") } catch {} }
    return [ordered]@{
        quando   = $quando
        de_nome  = [string]$m.SenderName
        de_email = $de
        assunto  = [string]$m.Subject
        nao_lida = ($m.UnRead -eq $true)
        tamanho  = [int]$m.Size
    }
}

$mtx = New-Object System.Threading.Mutex($false, "Global\PetAgentOutlookPoll")
$pegou = $false
try { $pegou = $mtx.WaitOne(0) } catch { $pegou = $true }   # sem mutex: segue, nao trava o dono
if (-not $pegou) { GravaStatus "ocupado" 0 "ja existe uma coleta rodando"; exit 0 }

$ol = $ns = $st = $inbox = $itens = $nl = $pa = $null
try {
    try { $ol = [Runtime.InteropServices.Marshal]::GetActiveObject("Outlook.Application") }
    catch { $ol = $null }

    if ($null -eq $ol) {
        # REGRA 1: nao subir o Outlook. Adia e sai.
        # ⚠️ MEDIDO 04/10: quando quem chama esta ELEVADO, o COM nao enxerga a
        # instancia do usuario e GetActiveObject falha igualzinho a "sem Outlook".
        # Nesse caso adiar em silencio seria mentira util: marca como erro.
        $elevado = $false
        try {
            $pr = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
            $elevado = $pr.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
        } catch {}
        if ($elevado) {
            GravaStatus "erro" 0 "processo ELEVADO: o COM do Outlook nao enxerga a instancia do usuario - rode nao-elevado (via explorer.exe)"
            Write-Output "ERRO: rodando elevado; o Outlook do usuario nao e visivel para o COM"
        } else {
            GravaStatus "sem_outlook" 0 "Outlook nao esta aberto - leitura adiada"
            Write-Output "ADIADO: Outlook nao esta aberto (nada foi iniciado)"
        }
        exit 0
    }

    $ns = $ol.GetNamespace("MAPI")
    $disponiveis = @()
    for ($s = 1; $s -le $ns.Stores.Count; $s++) {            # REGRA 4: indexado
        $stx = $ns.Stores.Item($s)
        try { $disponiveis += [string]$stx.DisplayName } finally { Solta $stx }
    }
    $st = $null
    for ($s = 1; $s -le $ns.Stores.Count; $s++) {
        $stx = $ns.Stores.Item($s)
        if ([string]$stx.DisplayName -eq $conta) { $st = $stx; break }
        Solta $stx
    }
    if ($null -eq $st) { throw ("store '" + $conta + "' nao encontrado. Existem: " + ($disponiveis -join " | ")) }

    $inbox = $st.GetDefaultFolder(6)
    $arr = New-Object System.Collections.ArrayList
    $vistos = New-Object System.Collections.ArrayList

    # 1) nao lidas (limite 60) — acesso indexado + solta cada MailItem na hora
    $nl = $inbox.Items.Restrict("[Unread]=True")
    $lim = [Math]::Min(60, $nl.Count)
    for ($i = 1; $i -le $lim; $i++) {
        $m = $nl.Item($i)
        try {
            $o = Cabecalho $m ([ref]$pa)
            [void]$arr.Add($o)
            [void]$vistos.Add($o.quando + "|" + $o.de_email + "|" + $o.assunto)
        } finally { Solta $pa; Solta $m }
    }

    # 2) contexto: 40 mais recentes, sem repetir
    $itens = $inbox.Items
    $itens.Sort("[ReceivedTime]", $true)
    $lim2 = [Math]::Min(40, $itens.Count)
    for ($k = 1; $k -le $lim2; $k++) {
        $m2 = $itens.Item($k)
        try {
            $o2 = Cabecalho $m2 ([ref]$pa)
            $fp = $o2.quando + "|" + $o2.de_email + "|" + $o2.assunto
            if ($vistos -notcontains $fp) { [void]$arr.Add($o2) }
        } finally { Solta $pa; Solta $m2 }
    }

    $ctx = "?"
    try {
        $pr = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
        $ctx = "elevado=" + $pr.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator) + " sessao=" + (Get-Process -Id $PID).SessionId
    } catch {}

    GravaJson $saida ([ordered]@{
        verificado_em      = (Get-Date).ToString("s")
        conta              = [string]$st.DisplayName
        contexto           = $ctx
        pasta              = [string]$inbox.Name
        total_pasta        = [int]$itens.Count
        nao_lidas          = [int]$inbox.UnReadItemCount
        lidas_agora        = $arr.Count
        contas_disponiveis = $disponiveis
        mensagens          = $arr
    })
    GravaStatus "ok" $arr.Count ""
    if (Test-Path $erroF) { Remove-Item $erroF -Force -ErrorAction SilentlyContinue }
    Write-Output ("OK: " + $arr.Count + " cabecalhos (" + $inbox.UnReadItemCount + " nao lidas) -> " + $saida)
}
catch {
    GravaStatus "erro" 0 $_.Exception.Message
    [System.IO.File]::WriteAllText($erroF, $_.Exception.Message, (New-Object System.Text.UTF8Encoding($false)))
    Write-Output ("ERRO: " + $_.Exception.Message)
}
finally {
    # REGRA 3: ordem inversa, tudo solto, mesmo em erro
    foreach ($obj in @($nl, $itens, $inbox, $st, $ns, $ol)) { Solta $obj }
    [GC]::Collect(); [GC]::WaitForPendingFinalizers()
    [GC]::Collect(); [GC]::WaitForPendingFinalizers()
    if ($pegou) { try { $mtx.ReleaseMutex() } catch {} }
    try { $mtx.Dispose() } catch {}
}
