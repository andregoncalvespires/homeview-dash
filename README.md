# HomeView

> Um painel (dashboard) leve e autohospedado para o seu homelab: login por e-mail com código, serviços organizados por grupos e links separados para acesso interno e externo.

[English](README.en.md) · **Português (Brasil)**

![licença MIT](https://img.shields.io/badge/licen%C3%A7a-MIT-blue) ![status](https://img.shields.io/badge/status-v0.1%20em%20desenvolvimento-orange)

## O que é

O HomeView é uma alternativa simples a painéis como Heimdall e Homarr, pensada para quem tem vários servidores, VMs e containers e quer **controlar quem vê o quê**.

- **Um único container** de imagem pequena, sem Redis nem Postgres. Os dados ficam em um arquivo SQLite.
- **Login sem senha**: só quem foi cadastrado pelo administrador entra, recebendo um código por e-mail. O navegador/aparelho fica autorizado por um cookie e pode ser revogado a qualquer momento.
- **Só administradores** adicionam usuários, grupos e serviços.
- **Dois endereços por serviço** (interno e externo). O painel destaca o endereço certo conforme você acessa de casa ou de fora, e mostra o outro como atalho.
- **Interface em português e inglês**, clara no tema claro e no escuro, com busca instantânea (tecle `/`).

### O que já funciona (v0.1)

- Login por código de e-mail + cookie por dispositivo, com tela "Meus dispositivos" para revogar.
- Administrador criado pelo console, **sem gravar nada no `docker-compose.yml`**.
- Usuários, grupos e serviços (nome, descrição, ícone, URL interna, URL externa, posição).
- Link de acesso único pelo console, para recuperação caso o e-mail falhe.

### O que vem a seguir

| Versão | Entrega |
|---|---|
| v0.2 | Descoberta automática de VMs e LXCs pela API do Proxmox, com aprovação pelo administrador |
| v0.3 | Agente leve para listar os containers Docker de cada máquina (labels `homeview.*` opcionais) |
| v0.4 | Importar/exportar YAML, visões por hierarquia/tipo, permissões por grupo |

## Instalação (3 passos)

Você precisa de Docker com Compose e de uma conta SMTP para enviar os códigos (veja [Configurando o e-mail](#configurando-o-e-mail)).

**1. Baixe os arquivos** em uma pasta, por exemplo `/opt/homeview`:

```bash
mkdir -p /opt/homeview && cd /opt/homeview
curl -fsSLO https://raw.githubusercontent.com/andregoncalvespires/homeview-dash/main/docker-compose.yml
curl -fsSL  https://raw.githubusercontent.com/andregoncalvespires/homeview-dash/main/.env.example -o .env
```

**2. Edite o `.env`** com o endereço do painel e os dados do SMTP, e suba:

```bash
nano .env
docker compose up -d
```

**3. Crie o administrador** (interativo; nada fica gravado no compose):

```bash
docker compose exec -it homeview homeview admin create
```

Abra o endereço configurado, digite o e-mail do administrador e informe o código recebido. Pronto: use o menu **Grupos**, **Serviços** e **Usuários** para montar o painel.

> A imagem é publicada em `ghcr.io/andregoncalvespires/homeview-dash`. Se o `docker compose up` negar acesso, o pacote ainda está privado: no GitHub, abra *Packages → homeview-dash → Package settings* e mude a visibilidade para **Public**. Também é possível compilar localmente: descomente a linha `build: .` no compose.

## Configuração

Tudo é feito por variáveis de ambiente, normalmente no arquivo `.env`.

| Variável | Padrão | Descrição |
|---|---|---|
| `HOMEVIEW_BASE_URL` | `http://localhost:8080` | Endereço público do painel. Com `https://`, o cookie recebe o atributo `Secure`. |
| `HOMEVIEW_PORT` | `8080` | Porta publicada no host (no compose). O container escuta sempre em 8080. |
| `HOMEVIEW_LANG` | `pt-BR` | Idioma padrão (`pt-BR` ou `en`). Cada usuário pode trocar no rodapé. |
| `HOMEVIEW_INTERNAL_HOSTS` | *(vazio)* | Nomes extras que significam "estou na rede local", separados por vírgula. |
| `HOMEVIEW_SMTP_HOST` | *(obrigatório)* | Servidor SMTP. |
| `HOMEVIEW_SMTP_PORT` | `587` | Porta SMTP. |
| `HOMEVIEW_SMTP_SECURITY` | `starttls` | `starttls` (587), `tls` (465) ou `none` (apenas relay local confiável). |
| `HOMEVIEW_SMTP_USER` / `HOMEVIEW_SMTP_PASS` | *(vazio)* | Credenciais, se o servidor exigir. |
| `HOMEVIEW_SMTP_FROM` | usuário SMTP | Remetente, por exemplo `HomeView <homeview@exemplo.com>`. |
| `HOMEVIEW_COOKIE_SECURE` | automático | Force `true`/`false` se necessário (por padrão vale `true` quando a URL base é https). |
| `HOMEVIEW_ADDR` / `HOMEVIEW_DATA` | `:8080` / `/data` | Endereço de escuta e pasta dos dados (já definidos na imagem). |
| `TZ` | `UTC` | Fuso horário exibido nas telas, ex.: `America/Sao_Paulo`. |

### Link interno × externo

Cada serviço pode ter uma **URL interna** (ex.: `http://192.168.1.10:8123`) e uma **URL externa** (ex.: `https://ha.exemplo.com`).

- Se você abrir o HomeView por um IP privado, nome sem ponto, `*.lan`, `*.local` ou um nome listado em `HOMEVIEW_INTERNAL_HOSTS`, o botão principal é o **interno** e o **externo** aparece como atalho.
- Por qualquer outro endereço (por exemplo, `https://dash.exemplo.com`), acontece o contrário.
- Se o serviço tem só uma URL, ela é sempre o botão principal.

Se você usa DNS interno com um nome "público" (ex.: `dash.casa.exemplo.com` apontando para um IP local), liste esse nome em `HOMEVIEW_INTERNAL_HOSTS`.

### Configurando o e-mail

O código de login é enviado por SMTP, então vale usar uma conta dedicada.

- **Gmail**: ative a verificação em duas etapas e crie uma *senha de app*. Use `smtp.gmail.com`, porta `587`, `starttls`, usuário = seu e-mail, senha = a senha de app.
- **Outros provedores** (Brevo, Mailgun, Office 365, seu próprio servidor): use os dados SMTP que eles fornecem. Para porta 465, defina `HOMEVIEW_SMTP_SECURITY=tls`.
- **Relay local sem criptografia** (ex.: Postfix na mesma rede): `HOMEVIEW_SMTP_SECURITY=none`. Não use isso em redes que você não controla.

O HomeView **recusa** enviar credenciais por conexão sem criptografia.

### Atrás de um proxy reverso ou Cloudflare Tunnel

1. Publique o container (porta 8080) pelo proxy/túnel e defina `HOMEVIEW_BASE_URL=https://dash.exemplo.com`.
2. O proxy deve **preservar o cabeçalho `Host`** (no Nginx: `proxy_set_header Host $host;`). O Cloudflare Tunnel já faz isso. É o `Host` que decide entre link interno e externo.
3. Use sempre HTTPS fora de casa.

## Uso diário

- **Atualizar**: `docker compose pull && docker compose up -d`.
- **Logs**: `docker compose logs -f homeview`. Se o código não chega, o motivo (por exemplo, falha de autenticação no SMTP) aparece aqui.
- **Não consigo entrar** (e-mail fora do ar): gere um link único no console, válido por 15 minutos:

  ```bash
  docker compose exec -it homeview homeview admin login-link --email voce@exemplo.com
  ```

- **Listar usuários**: `docker compose exec homeview homeview admin list`.
- **Corrigir o nome de um usuário** (por exemplo, acentos gravados errado):
  `docker compose exec homeview homeview admin rename --email voce@exemplo.com --name "André Pires"`.
  Dica: se o terminal não estiver em UTF-8, digitar acentos no prompt interativo pode corromper o texto; passe o nome com `--name` (também aceito em `admin create`) ou configure o terminal para UTF-8. O HomeView recusa nomes com codificação inválida.
- **Backup**: todos os dados estão no volume `homeview-data` (arquivo `homeview.db`). Por exemplo:

  ```bash
  docker compose stop homeview
  docker run --rm -v "$(docker volume ls -q | grep homeview-data)":/data -v "$PWD":/backup alpine \
    tar czf /backup/homeview-data.tgz -C /data .
  docker compose start homeview
  ```

- Se preferir uma pasta do host em vez de volume nomeado, ela precisa pertencer ao UID `10001` (`chown 10001:10001 /caminho`).

## Segurança

- Só e-mails **cadastrados pelo administrador** conseguem receber código; a resposta da tela de login é idêntica para e-mails desconhecidos.
- Códigos de 6 dígitos valem 10 minutos, têm no máximo 5 tentativas e são limitados a 3 envios por 15 minutos por usuário. Ficam guardados apenas como hash (HMAC).
- O cookie do dispositivo é um valor aleatório de 256 bits; no banco fica só o hash dele. Cada aparelho pode ser revogado em **Meus dispositivos**, e dispositivos inativos por 180 dias expiram.
- Formulários protegidos contra CSRF, cabeçalhos de segurança (CSP restritiva, `X-Frame-Options`, etc.) e URLs aceitas apenas com `http://` ou `https://`.
- A segurança do login depende da segurança da conta de e-mail de cada usuário. Não exponha o painel à internet sem HTTPS.

Encontrou uma falha? Abra uma *issue* sem detalhes exploráveis ou envie uma mensagem privada ao mantenedor.

## Versões

As versões são publicadas automaticamente, sem criar tags à mão. A fonte da verdade é o arquivo [`VERSION`](VERSION) na raiz do repositório.

**Para publicar uma versão nova:**

1. Altere o número em `VERSION` (por exemplo, `0.1.1`) no mesmo commit das mudanças, ou em um commit à parte. Na primeira publicação, crie o arquivo contendo apenas `0.1.0`.
2. Faça push para a `main`.

O workflow [`release.yml`](.github/workflows/release.yml) então:

- lê o `VERSION` e, se a release `v<versão>` ainda não existir, cria a **tag** e a **release** no GitHub, com notas geradas a partir dos commits;
- constrói e publica a **imagem Docker** (amd64 e arm64) em `ghcr.io/andregoncalvespires/homeview-dash` com as tags `<versão>`, `<major>.<minor>` e `latest`. Versões com sufixo, como `0.2.0-rc.1`, viram *pré-lançamento* e não movem `latest`.

Se a release já existir, nada é recriado, então repetir o push é seguro. Para refazer só a imagem (por exemplo, se a construção falhou), use **Actions → Release → Run workflow**.

> **Pré-requisito (uma vez):** em *Settings → Actions → General → Workflow permissions*, escolha **Read and write permissions**. Depois da primeira imagem publicada, torne o pacote público em *Packages → homeview-dash → Package settings*.
>
> A construção da imagem fica no mesmo workflow da release porque tags criadas pelo `GITHUB_TOKEN` não disparam outros workflows.

## Desenvolvimento

Requer a versão do Go indicada em `go.mod` (atualmente 1.26).

```bash
go mod tidy
go vet ./... && go test ./...
HOMEVIEW_SMTP_HOST=localhost HOMEVIEW_SMTP_SECURITY=none HOMEVIEW_SMTP_FROM=dev@localhost HOMEVIEW_DATA=./data go run ./cmd/homeview
```

Estrutura: `cmd/homeview` (ponto de entrada), `internal/auth` (códigos e dispositivos), `internal/web` (páginas, `templates/` e `static/`), `internal/store` (interface e SQLite), `internal/i18n` (textos pt-BR/EN). Para traduzir para outro idioma, copie um dos blocos em `internal/i18n/i18n.go`; os testes avisam se faltar alguma chave.

## Licença

[MIT](LICENSE)
