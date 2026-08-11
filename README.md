# Madden 27 Inject

Utilitário para Windows que localiza a instalação do Madden NFL 27 na Steam, baixa o pacote configurado no projeto e instala os arquivos na pasta do jogo.

Durante a instalação, o programa verifica o SHA-256 do download e preserva o executável original como `Madden27_original.exe` antes de ativar o executável corrigido.

## Requisitos

- Windows 10 ou Windows 11;
- Madden NFL 27 instalado pela Steam;
- conexão com a internet;
- permissão para escrever na pasta do jogo;
- jogo e Steam fechados durante a instalação.

## Como usar

1. Acesse a página de [Releases](https://github.com/YlanzinhoY/m27-inject/releases).
2. Baixe o arquivo `m27-inject.exe` da versão mais recente.
3. Feche o Madden NFL 27 e a Steam.
4. Execute `m27-inject.exe`.
5. Escolha uma das opções na interface:
   - **Procurar automaticamente na Steam**: procura o jogo nas bibliotecas configuradas na Steam;
   - **Informar o caminho completo**: permite informar manualmente a pasta `Madden NFL 27`.
6. Aguarde o download, a extração e a instalação terminarem.

Exemplo de caminho informado manualmente:

```text
C:\Program Files (x86)\Steam\steamapps\common\Madden NFL 27
```

Use as setas para navegar, `Enter` para confirmar e `Esc` para voltar. Durante a instalação, `Ctrl+C` cancela o programa.

## O que o programa faz

1. Baixa o arquivo RAR para uma área temporária.
2. Valida o checksum SHA-256 do arquivo baixado.
3. Extrai o conteúdo para uma pasta temporária.
4. Move `preloader_l.dll` da subpasta do pacote para a raiz dos arquivos extraídos.
5. Renomeia o `Madden27.exe` instalado para `Madden27_original.exe`.
6. Renomeia o `Madden27.fixed.exe` extraído para `Madden27.exe`.
7. Copia os arquivos extraídos para a pasta do jogo.

O programa não sobrescreve um `Madden27_original.exe` existente. Essa proteção evita a perda de um backup criado anteriormente.

## Restaurar o executável original

Para restaurar somente o executável original:

1. Feche o jogo e a Steam.
2. Retire o `Madden27.exe` atual da pasta do jogo.
3. Renomeie `Madden27_original.exe` para `Madden27.exe`.

Antes de alterar os arquivos manualmente, confirme que `Madden27_original.exe` é realmente o backup original.

## Solução de problemas

### O jogo não foi encontrado automaticamente

Escolha **Informar o caminho completo** e selecione a pasta que termina em `steamapps\common\Madden NFL 27`.

### O backup já existe

O programa interrompe a instalação para não sobrescrever `Madden27_original.exe`. Verifique os executáveis existentes e restaure ou guarde o backup antes de tentar novamente.

### Falha de checksum

O download recebido é diferente do pacote esperado. Execute novamente para fazer um novo download. O programa não instala um arquivo cujo SHA-256 seja inválido.

### Falha de permissão

Confira se o jogo e a Steam estão fechados. Se a pasta continuar bloqueada, execute o programa com uma conta que tenha permissão de escrita sobre a instalação do jogo.

## Desenvolvimento

O projeto usa Go 1.26.1.

```powershell
go mod download
go test ./...
go build -o bin/m27-inject.exe .
```

## Licença

Distribuído sob a [Apache License 2.0](LICENSE).

Este é um projeto independente e não possui afiliação com EA, Madden NFL ou Steam.
