[CmdletBinding()]
param(
  [Parameter(Mandatory)][string]$CertificateZip,
  [Parameter(Mandatory)][string]$WechatPublicKeyFile,
  [Parameter(Mandatory)][string]$ApiV3KeyFile,
  [Parameter(Mandatory)][string]$AppSecretFile,
  [string]$IdentityFile = 'C:\Users\19918\.ssh\xiaolan-ecs-deploy',
  [string]$OperationId = [guid]::NewGuid().ToString()
)

# Credential contents travel only over encrypted SSH stdin, never arguments or logs.
$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if ([guid]::Parse($OperationId).ToString() -cne $OperationId) { throw 'Invalid operation ID.' }
$remoteDirectory = '/tmp/xiaolan-wechat-config-' + $OperationId
$remoteHelper = $remoteDirectory + '/stage-wechatpay-credentials.py'
$remoteBinary = $remoteDirectory + '/wechatpay-preflight'
$remote = 'ecs-user@47.114.55.117'
$sshArguments = @('-i', $IdentityFile, '-o', 'IdentitiesOnly=yes', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10', '-o', 'StrictHostKeyChecking=yes')
$helperPath = Join-Path $PSScriptRoot 'server\stage-wechatpay-credentials.py'
$binaryPath = Join-Path $repoRoot ('artifacts\wechatpay-preflight-' + $OperationId)

function Invoke-SafeProcess([string]$Program, [string[]]$Arguments, [string]$InputText = '') {
  $start = [Diagnostics.ProcessStartInfo]::new()
  $start.FileName = $Program
  $start.UseShellExecute = $false
  $start.CreateNoWindow = $true
  $start.RedirectStandardInput = $true
  $start.RedirectStandardOutput = $true
  $start.RedirectStandardError = $true
  $start.StandardInputEncoding = [Text.UTF8Encoding]::new($false)
  foreach ($argument in $Arguments) { $start.ArgumentList.Add($argument) }
  $process = [Diagnostics.Process]::new()
  $process.StartInfo = $start
  try {
    if (-not $process.Start()) { throw 'Process could not start.' }
    $outputTask = $process.StandardOutput.ReadToEndAsync()
    $errorTask = $process.StandardError.ReadToEndAsync()
    if ($InputText) { $process.StandardInput.Write($InputText) }
    $process.StandardInput.Close()
    if (-not $process.WaitForExit(55000)) {
      $process.Kill($true)
      throw 'Process timed out; inspect status before retrying.'
    }
    $process.WaitForExit()
    $output = $outputTask.GetAwaiter().GetResult()
    $null = $errorTask.GetAwaiter().GetResult()
    # Do not emit stderr or exception details from a credential-processing child.
    return [pscustomobject]@{ExitCode=$process.ExitCode; Output=$output}
  } finally { $process.Dispose() }
}

$remoteCreated = $false
$payloadJSON = $null
$zip = $null
try {
  foreach ($credentialPath in @($CertificateZip, $WechatPublicKeyFile, $ApiV3KeyFile, $AppSecretFile)) {
    $absolute = [IO.Path]::GetFullPath($credentialPath)
    if ($absolute.StartsWith($repoRoot.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase)) {
      throw 'Credentials must remain outside the source repository.'
    }
    if (-not [IO.File]::Exists($absolute)) { throw 'Required credential file is missing.' }
  }
  $apiKey = [IO.File]::ReadAllText($ApiV3KeyFile).Trim()
  $appSecret = [IO.File]::ReadAllText($AppSecretFile).Trim()
  if ($apiKey -cnotmatch '^[A-Za-z0-9]{32}$' -or $appSecret -cnotmatch '^[A-Za-z0-9]{32}$' -or $apiKey -ceq $appSecret) {
    throw 'Credential format verification failed.'
  }
  $zip = [IO.Compression.ZipFile]::OpenRead($CertificateZip)
  $materials = @{}
  foreach ($name in @('apiclient_key.pem', 'apiclient_cert.pem')) {
    $entry = $zip.GetEntry($name)
    if (-not $entry -or $entry.Length -gt 16384) { throw 'Certificate archive is invalid.' }
    $reader = [IO.StreamReader]::new($entry.Open(), [Text.Encoding]::ASCII)
    try { $materials[$name] = $reader.ReadToEnd() } finally { $reader.Dispose() }
  }
  $zip.Dispose()
  $zip = $null
  $materials['pub_key.pem'] = [IO.File]::ReadAllText($WechatPublicKeyFile)
  $certificate = [Security.Cryptography.X509Certificates.X509Certificate2]::CreateFromPem($materials['apiclient_cert.pem'])
  try {
    if ($certificate.SerialNumber -cne '1FC20AC9C1A7593834C0DCDC03405E6B2960773B' -or $certificate.Subject -notmatch 'CN=1118326239' -or $certificate.NotAfter -lt (Get-Date)) {
      throw 'Merchant certificate metadata verification failed.'
    }
  } finally { $certificate.Dispose() }
  $merchantRSA = [Security.Cryptography.RSA]::Create()
  $certificateRSA = [Security.Cryptography.X509Certificates.X509Certificate2]::CreateFromPem($materials['apiclient_cert.pem'])
  $publicRSA = [Security.Cryptography.RSA]::Create()
  $certificatePublicRSA = $null
  try {
    $merchantRSA.ImportFromPem($materials['apiclient_key.pem'])
    $certificatePublicRSA = [Security.Cryptography.X509Certificates.RSACertificateExtensions]::GetRSAPublicKey($certificateRSA)
    if (-not [Security.Cryptography.CryptographicOperations]::FixedTimeEquals($merchantRSA.ExportSubjectPublicKeyInfo(), $certificatePublicRSA.ExportSubjectPublicKeyInfo())) {
      throw 'Merchant private key does not match certificate.'
    }
    $publicRSA.ImportFromPem($materials['pub_key.pem'])
    $fingerprint = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($publicRSA.ExportSubjectPublicKeyInfo()))
    if ($publicRSA.KeySize -ne 2048 -or $fingerprint -cne '55BF13779958B35B147A846E950756C471521B0EA9EED237D27ECD1EC1E4B86E') {
      throw 'Wechat public key does not match verified file.'
    }
  } finally {
    if ($certificatePublicRSA) { $certificatePublicRSA.Dispose() }
    $merchantRSA.Dispose(); $certificateRSA.Dispose(); $publicRSA.Dispose()
  }

  $priorTargetOS = $env:GOOS
  $priorTargetArchitecture = $env:GOARCH
  $priorCGO = $env:CGO_ENABLED
  try {
    $env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
    Push-Location (Join-Path $repoRoot 'management-service')
    try {
      & go build -trimpath -o $binaryPath ./cmd/wechatpay-preflight
      if ($LASTEXITCODE -ne 0) { throw 'Preflight build failed.' }
    } finally { Pop-Location }
  } finally {
    $env:GOOS = $priorTargetOS; $env:GOARCH = $priorTargetArchitecture; $env:CGO_ENABLED = $priorCGO
  }

  $ssh = 'C:\WINDOWS\System32\OpenSSH\ssh.exe'
  $scp = 'C:\WINDOWS\System32\OpenSSH\scp.exe'
  $created = Invoke-SafeProcess $ssh ($sshArguments + @($remote, "mkdir -m 700 $remoteDirectory"))
  if ($created.ExitCode -ne 0) { throw 'Remote preparation failed.' }
  $remoteCreated = $true
  foreach ($upload in @(@($helperPath, $remoteHelper), @($binaryPath, $remoteBinary))) {
    $uploaded = Invoke-SafeProcess $scp ($sshArguments + @($upload[0], ($remote + ':' + $upload[1])))
    if ($uploaded.ExitCode -ne 0) { throw 'Public preflight helper upload failed.' }
  }
  $permission = Invoke-SafeProcess $ssh ($sshArguments + @($remote, "chmod 700 $remoteBinary"))
  if ($permission.ExitCode -ne 0) { throw 'Preflight permissions failed.' }

  $payloadJSON = @{
    operation_id = $OperationId
    materials = $materials
    environment = @{
      WECHAT_PAY_APP_ID = 'wx5a19fd75c13d5584'
      WECHAT_PAY_MCH_ID = '1118326239'
      WECHAT_PAY_MERCHANT_CERT_SERIAL_NO = '1FC20AC9C1A7593834C0DCDC03405E6B2960773B'
      WECHAT_PAY_API_V3_KEY = $apiKey
      WECHAT_OFFICIAL_ACCOUNT_APP_SECRET = $appSecret
      WECHAT_PAY_PUBLIC_KEY_ID = 'PUB_KEY_ID_0111183262392026100300211816001000'
      WECHAT_PAY_NOTIFY_URL = 'https://www.xiaolandaizi.cn/api/v1/payments/wechat/notify'
      WECHAT_PAY_OAUTH_CALLBACK_URL = 'https://www.xiaolandaizi.cn/api/v1/payments/wechat/oauth/callback'
    }
  } | ConvertTo-Json -Depth 5 -Compress
  $staged = Invoke-SafeProcess $ssh ($sshArguments + @($remote, "sudo -n python3 -B $remoteHelper")) $payloadJSON
  $payloadJSON = $null
  if ($staged.ExitCode -ne 0) { throw 'Server staging did not complete; inspect safe state before retrying.' }
  $stageReport = $staged.Output | ConvertFrom-Json
  if (-not $stageReport.staged -or $stageReport.productionPaymentEnabled -or $stageReport.sensitiveValuesPrinted) {
    throw 'Unexpected staging report.'
  }
  $stageReport | ConvertTo-Json -Compress
  $checked = Invoke-SafeProcess $ssh ($sshArguments + @($remote, "sudo -n python3 -B $remoteHelper preflight $remoteBinary"))
  $safeReport = $checked.Output | ConvertFrom-Json
  if ($safeReport.sensitiveValuesPrinted -or $safeReport.createdPaymentOrder -or $safeReport.productionPaymentEnabled) {
    throw 'Unexpected preflight report.'
  }
  $safeReport | ConvertTo-Json -Compress
  if ($checked.ExitCode -ne 0) { throw 'Credential preflight requires attention; production payment remains disabled.' }
} finally {
  if ($zip) { $zip.Dispose() }
  $payloadJSON = $null; $apiKey = $null; $appSecret = $null; $materials = $null
  if ($remoteCreated) {
    # Remove only these two public temporary helpers; retain credentials and backup.
    $cleanup = Invoke-SafeProcess 'C:\WINDOWS\System32\OpenSSH\ssh.exe' ($sshArguments + @($remote, "rm -- $remoteHelper $remoteBinary; rmdir -- $remoteDirectory"))
    if ($cleanup.ExitCode -ne 0) { Write-Warning 'Public temporary helper cleanup needs attention.' }
  }
}
