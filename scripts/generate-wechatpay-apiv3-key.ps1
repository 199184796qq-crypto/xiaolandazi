[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$SecretDirectory
)

# This tool creates a private runtime credential, never source-code configuration.
# Do not add the generated file to Git, print it, or include it in diagnostic logs.
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
    throw 'This credential generator requires Windows ACL support.'
}
if ($PSVersionTable.PSVersion.Major -lt 7) {
    throw 'Run this credential generator with PowerShell 7 or newer.'
}
if (-not [IO.Path]::IsPathFullyQualified($SecretDirectory)) {
    throw 'Use an absolute directory outside the source-code checkout.'
}

$taskDirectoryPath = [IO.Path]::GetFullPath($SecretDirectory)
$taskRepositoryRoot = [IO.DirectoryInfo]::new($PSScriptRoot).Parent.FullName
if ($taskDirectoryPath.Equals($taskRepositoryRoot, [StringComparison]::OrdinalIgnoreCase) -or
    $taskDirectoryPath.StartsWith($taskRepositoryRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Credentials must be stored outside the source-code checkout.'
}

$taskDirectory = [IO.DirectoryInfo]::new($taskDirectoryPath)
$taskAncestor = $taskDirectory
while ($null -ne $taskAncestor) {
    if ($taskAncestor.Exists -and ($taskAncestor.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
        throw 'Credential directory ancestors must not be reparse points.'
    }
    $taskAncestor = $taskAncestor.Parent
}
if ($null -eq $taskDirectory.Parent -or -not $taskDirectory.Parent.Exists) {
    throw 'The parent of the credential directory must already exist.'
}
if ($taskDirectory.Exists) {
    throw 'Credential directory already exists; refusing to alter or replace credentials.'
}

$taskIdentity = [Security.Principal.WindowsIdentity]::GetCurrent()
try { $taskUserSid = $taskIdentity.User } finally { $taskIdentity.Dispose() }
$taskSystemSid = [Security.Principal.SecurityIdentifier]::new('S-1-5-18')
$taskInheritance = [Security.AccessControl.InheritanceFlags]::ContainerInherit -bor [Security.AccessControl.InheritanceFlags]::ObjectInherit
$taskDirectorySecurity = [Security.AccessControl.DirectorySecurity]::new()
$taskDirectorySecurity.SetAccessRuleProtection($true, $false)
$taskDirectorySecurity.SetOwner($taskUserSid)
foreach ($taskAllowedSid in @($taskUserSid, $taskSystemSid)) {
    $taskDirectoryRule = [Security.AccessControl.FileSystemAccessRule]::new(
        $taskAllowedSid, [Security.AccessControl.FileSystemRights]::FullControl,
        $taskInheritance, [Security.AccessControl.PropagationFlags]::None,
        [Security.AccessControl.AccessControlType]::Allow
    )
    $taskDirectorySecurity.AddAccessRule($taskDirectoryRule)
}
[IO.FileSystemAclExtensions]::Create($taskDirectory, $taskDirectorySecurity)

$taskSecretPath = [IO.Path]::Combine($taskDirectoryPath, 'WECHAT_PAY_API_V3_KEY.txt')
$taskFileSecurity = [Security.AccessControl.FileSecurity]::new()
$taskFileSecurity.SetAccessRuleProtection($true, $false)
$taskFileSecurity.SetOwner($taskUserSid)
foreach ($taskAllowedSid in @($taskUserSid, $taskSystemSid)) {
    $taskFileRule = [Security.AccessControl.FileSystemAccessRule]::new(
        $taskAllowedSid, [Security.AccessControl.FileSystemRights]::FullControl,
        [Security.AccessControl.AccessControlType]::Allow
    )
    $taskFileSecurity.AddAccessRule($taskFileRule)
}

$taskRandom = [Security.Cryptography.RandomNumberGenerator]::Create()
$taskRandomByte = [byte[]]::new(1)
$taskSecretBytes = $null
$taskSecretChars = [char[]]::new(32)
$taskSecretText = $null
$taskAlphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789'
try {
    do {
        for ($taskIndex = 0; $taskIndex -lt 32; $taskIndex++) {
            do { $taskRandom.GetBytes($taskRandomByte) } while ($taskRandomByte[0] -ge 248)
            $taskSecretChars[$taskIndex] = $taskAlphabet[$taskRandomByte[0] % 62]
        }
        $taskSecretText = [string]::new($taskSecretChars)
    } while ($taskSecretText -cnotmatch '[A-Z]' -or $taskSecretText -cnotmatch '[a-z]' -or $taskSecretText -notmatch '[0-9]')

    $taskSecretBytes = [Text.Encoding]::ASCII.GetBytes($taskSecretText)
    # ACL is supplied at creation, before any credential bytes are written.
    $taskSecretStream = [IO.FileSystemAclExtensions]::Create(
        [IO.FileInfo]::new($taskSecretPath), [IO.FileMode]::CreateNew,
        [Security.AccessControl.FileSystemRights]::Write,
        [IO.FileShare]::None, 4096, [IO.FileOptions]::WriteThrough, $taskFileSecurity
    )
    try {
        $taskSecretStream.Write($taskSecretBytes, 0, $taskSecretBytes.Length)
        $taskSecretStream.Flush($true)
    } finally { $taskSecretStream.Dispose() }
} finally {
    $taskRandom.Dispose()
    [Array]::Clear($taskRandomByte, 0, $taskRandomByte.Length)
    [Array]::Clear($taskSecretChars, 0, $taskSecretChars.Length)
    if ($null -ne $taskSecretBytes) { [Array]::Clear($taskSecretBytes, 0, $taskSecretBytes.Length) }
    $taskSecretText = $null
}

[PSCustomObject]@{
    Path = $taskSecretPath
    Characters = 32
    Created = $true
    Access = 'Current Windows user and SYSTEM only'
    SecretPrinted = $false
}
