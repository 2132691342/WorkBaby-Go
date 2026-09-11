<#
.SYNOPSIS
  分层测试入口。

.DESCRIPTION
  改一个小功能不必跑全量：
    默认   核心后端包（service / harness / llm / tool）+ 前端类型检查与单测
    -Pkg   只跑指定后端包（如 -Pkg service 或 -Pkg service,harness）
    -Full  全量后端 + 依赖方向门禁 + 前端

.EXAMPLE
  powershell scripts/test.ps1
  powershell scripts/test.ps1 -Pkg harness
  powershell scripts/test.ps1 -Full
#>
param(
  [string]$Pkg = '',
  [switch]$Full
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

# 指定包：最小闭环，只跑这一个域
if ($Pkg) {
  $targets = @($Pkg.Split(',') | ForEach-Object { "./internal/$($_.Trim())/..." })
  Write-Host "== go test $($targets -join ' ') ==" -ForegroundColor Cyan
  go test $targets
  exit $LASTEXITCODE
}

if ($Full) {
  Write-Host '== go test ./... ==' -ForegroundColor Cyan
  go test ./...
  if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

  Write-Host '== 依赖方向门禁 ==' -ForegroundColor Cyan
  powershell -ExecutionPolicy Bypass -File scripts/check-boundaries.ps1
  if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} else {
  Write-Host '== go test 核心包 ==' -ForegroundColor Cyan
  go test ./internal/service/... ./internal/harness/... ./internal/llm/... ./internal/tool/...
  if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}

Write-Host '== 前端类型检查 ==' -ForegroundColor Cyan
Push-Location frontend
npx vue-tsc --noEmit -p tsconfig.json
if ($LASTEXITCODE -ne 0) { Pop-Location; exit $LASTEXITCODE }

Write-Host '== 前端单测 ==' -ForegroundColor Cyan
npx vitest run
$viteExit = $LASTEXITCODE
Pop-Location

if ($viteExit -ne 0) { exit $viteExit }
Write-Host '全部通过' -ForegroundColor Green
