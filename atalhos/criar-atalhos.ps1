# criar-atalhos.ps1 — cria o atalho da AREA DE TRABALHO e o de INICIO AUTOMATICO
# do PetAgent, apontando para o lancador (lancar.vbs) que fica com o app.
#
#   powershell -ExecutionPolicy Bypass -File criar-atalhos.ps1
#
# Aditivo e reversivel: nao mexe em nada existente, so cria dois .lnk. Para
# desfazer, apague os dois atalhos ("PetAgent.lnk" na area de trabalho e na pasta
# Inicializacao) — o app e o lancador ficam onde estao.
# ASCII puro: o PowerShell 5.1 le .ps1 como ANSI e acento quebra o parse.

$ErrorActionPreference = "Stop"
$dest  = Join-Path $env:LOCALAPPDATA "petagent"
$vbs   = Join-Path $dest "lancar.vbs"
$exe   = Join-Path $dest "petagent.exe"

if (-not (Test-Path $exe)) {
    Write-Host "! o app nao esta instalado em $exe"
    Write-Host "  rode antes:  hermes petagent instalar"
    exit 1
}

# o lancador vem junto deste script (atalhos/lancar.vbs) e e copiado para a pasta do app
$origemVbs = Join-Path $PSScriptRoot "lancar.vbs"
if (-not (Test-Path $vbs)) {
    if (Test-Path $origemVbs) {
        Copy-Item $origemVbs $vbs -Force
        Write-Host "ok: lancador copiado para $vbs"
    } else {
        Write-Host "! nao achei o lancador (nem em $vbs nem em $origemVbs)"
        exit 1
    }
}

$sh = New-Object -ComObject WScript.Shell
$pastas = @(
    @{ nome = "area de trabalho"; caminho = [Environment]::GetFolderPath("Desktop") },
    @{ nome = "inicio automatico"; caminho = [Environment]::GetFolderPath("Startup") }
)

foreach ($p in $pastas) {
    if (-not (Test-Path $p.caminho)) { Write-Host "! pasta nao existe: $($p.caminho)"; continue }
    $atalho = Join-Path $p.caminho "PetAgent.lnk"
    $lnk = $sh.CreateShortcut($atalho)
    $lnk.TargetPath       = "wscript.exe"
    $lnk.Arguments        = '"' + $vbs + '"'
    $lnk.WorkingDirectory = $dest
    $lnk.Description      = "Coloca o boneco do PetAgent na tela (nao abre um segundo se ja estiver)"
    $lnk.IconLocation     = "$exe,0"
    $lnk.WindowStyle      = 1
    $lnk.Save()
    Write-Host "ok: $($p.nome) -> $atalho"
}

Write-Host ""
Write-Host "pronto. Para desfazer: apague os dois PetAgent.lnk (area de trabalho e Inicializacao)."
