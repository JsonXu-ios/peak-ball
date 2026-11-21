$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$modelEnv = Join-Path $projectRoot 'data/draw-model/python-env'
$modelPython = Join-Path $modelEnv 'Scripts/python.exe'
if (-not (Test-Path -LiteralPath $modelPython)) {
    python -m venv $modelEnv
    if ($LASTEXITCODE -ne 0) { throw '创建模型 Python 环境失败' }
}
& $modelPython -m pip install -r (Join-Path $PSScriptRoot 'draw-runtime-requirements.txt')
if ($LASTEXITCODE -ne 0) { throw '安装模型训练依赖失败' }
& $modelPython -c 'import sklearn, numpy, scipy; print("模型训练环境就绪", sklearn.__version__)'
if ($LASTEXITCODE -ne 0) { throw '模型训练依赖检查失败' }
Write-Output '环境准备完成。未启动或重启任何程序。'
