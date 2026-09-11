<#
.SYNOPSIS
  分层测试入口。

.DESCRIPTION
  改一个小功能不必跑全量：
    默认       核心后端包（service / harness / llm / tool）+ 前端类型检查与单测
    -Pkg       只跑指定后端包（如 -Pkg service 或 -Pkg service,harness）
    -Full      全量后端 + 依赖方向门禁 + 前端
    -Short     把 -short 传给 go test（跳过 timing-sensitive flake 测试；CI 推荐用）

.EXAMPLE
  powershell scripts/test.ps1
  powershell scripts/test.ps1 -Pkg harness
  powershell scripts/test.ps1 -Full
  powershell scripts/test.ps1 -Full -Short   # CI：跳过 flake 测试
#>
param(
  [string]$Pkg = '',
  [switch]$Full,
  [switch]$Short
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$shortFlag = if ($Short) { '-short' } else { '' }

# 指定包：最小闭环，只跑这一个域
if ($Pkg) {
  $targets = @($Pkg.Split(',') | ForEach-Object { "./internal/$($_.Trim())/..." })
  Write-Host "== go test $($targets -join ' ') $shortFlag==" -ForegroundColor Cyan
  go test $targets @shortFlag.Split(' ')
  exit $LASTEXITCODE
}

if ($Full) {
  # 注：CI 不能用 `go test ./...` —— main 包通过 go:embed 依赖 frontend/dist，
  # CI 流水线在跑 wails build 之前没构建前端；编译 main 包会触发 setup failed。
  # 测试用例都在 internal/ 包里，main 包是 Wails 入口不含测试；排除 main 即可。
  Write-Host '== go test ./internal/...（排除 main 包）==' -ForegroundColor Cyan
  $testArgs = @('./internal/...')
  if ($Short) { $testArgs += '-short' }
  go test @testArgs
  if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

  Write-Host '== 依赖方向门禁 ==' -ForegroundColor Cyan
  powershell -ExecutionPolicy Bypass -File scripts/check-boundaries.ps1
  if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} else {
  Write-Host '== go test 核心包 ==' -ForegroundColor Cyan
  $testArgs = @('./internal/service/...', './internal/harness/...', './internal/llm/...', './internal/tool/...')
  if ($Short) { $testArgs += '-short' }
  go test @testArgs
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
