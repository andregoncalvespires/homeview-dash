# HomeView

> Um painel (dashboard) leve e autohospedado para o seu homelab: login por e-mail com código, serviços organizados por grupos e links separados para acesso interno e externo.

[English](README.en.md) · **Português (Brasil)**

![licença MIT](https://img.shields.io/badge/licen%C3%A7a-MIT-blue) ![status](https://img.shields.io/badge/status-v0.5%20em%20desenvolvimento-orange)

## O que é

O HomeView é uma alternativa simples a painéis como Heimdall e Homarr, pensada para quem tem vários servidores, VMs e containers e quer **controlar quem vê o quê**.

- **Um único container** de imagem pequena, sem Redis nem Postgres. Os dados ficam em um arquivo SQLite.
- **Login sem senha**: só quem foi cadastrado pelo administrador entra, recebendo um código por e-mail. O navegador/aparelho fica autorizado por um cookie e pode ser revogado a qualquer momento.
- **Só administradores** adicionam usuários, grupos e serviços.
- **Dois endereços por serviço** (interno e externo). O painel destaca o endereço certo conforme você acessa de casa ou de fora, e mostra o outro como atalho.
- **Interface em português e inglês**, clara no tema claro e no escuro, com busca instantânea (tecle `/`).

### O que já funciona (v0.5)

- **Login sem senha**: código por e-mail + cookie por dispositivo, com a tela "Meus dispositivos" para revogar. Link de acesso único pelo console, para recuperação caso o e-mail falhe.
- **Administrador criado pelo console**, sem gravar nada no `docker-compose.yml`.
- **Usuários, grupos e serviços** (nome, descrição, ícone, URL interna, URL externa), com **catálogo de 35 aplicações** (ícone e porta sugerida) ou **ícone próprio** enviado do computador.
- **Descoberta pelo Proxmox**: lê as VMs e LXCs do seu servidor ou cluster; você aprova o que vai para o painel, **com vários serviços por VM/LXC** e a possibilidade de apontar para o **IP do próprio host Proxmox**. O ponto verde/cinza mostra se estão rodando.
- **Três visões do painel** (ícone de camadas ao lado da engrenagem): **Grupos** (os seus), **Infraestrutura** (por VM/LXC do Proxmox, com status e IP) e **Tipo** (Automação, Rede, Mídia…). A escolha fica salva no navegador.
- **Painel organizável**: reorganizar arrastando cards e grupos (duas colunas de grupos), **recolher/expandir grupos** (lembrado no navegador) e busca instantânea.
- **Editor YAML** de grupos e serviços, com validação antes de aplicar e download do arquivo.
- **Aparência por usuário**: tema claro/escuro, cor de destaque e 11 fundos prontos (céu estrelado e tema escuro são o padrão) ou imagem própria.
- **Menu em engrenagem** (⚙) no cabeçalho com todas as telas.

### O que vem a seguir

| Versão | Entrega |
|---|---|
| v0.6 | Subgrupos, tipo editável por serviço, nó Proxmox na visão Infraestrutura e permissões por grupo |

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

As configurações de execução (endereço, SMTP, idioma…) são variáveis de ambiente, normalmente no arquivo `.env`. Já usuários, grupos, serviços e conexões ficam no banco SQLite `homeview.db`, dentro do volume Docker `homeview-data` (`/data`): não há arquivo de configuração ao lado do compose.

| Variável | Padrão | Descrição |
|---|---|---|
| `HOMEVIEW_BASE_URL` | `http://localhost:8080` | Endereço público do painel. Com `https://`, o cookie recebe o atributo `Secure`. |
| `HOMEVIEW_PORT` | `8080` | Porta publicada no host (no compose). O container escuta sempre em 8080. |
| `HOMEVIEW_LANG` | `pt-BR` | Idioma padrão (`pt-BR` ou `en`). Cada usuário pode trocar no rodapé. |
| `HOMEVIEW_DISCOVERY_INTERVAL` | `10m` | De quanto em quanto tempo reler as conexões Proxmox (`30m`, `1h`…, mínimo `1m`; `0` desliga e deixa só o botão "Sincronizar agora"). |
| `HOMEVIEW_SMTP_HOST` | *(obrigatório)* | Servidor SMTP. |
| `HOMEVIEW_SMTP_PORT` | `587` | Porta SMTP. |
| `HOMEVIEW_SMTP_SECURITY` | `starttls` | `starttls` (587), `tls` (465) ou `none` (apenas relay local confiável). |
| `HOMEVIEW_SMTP_USER` / `HOMEVIEW_SMTP_PASS` | *(vazio)* | Credenciais, se o servidor exigir. |
| `HOMEVIEW_SMTP_FROM` | usuário SMTP | Remetente, por exemplo `HomeView <homeview@exemplo.com>`. |
| `HOMEVIEW_COOKIE_SECURE` | automático | Force `true`/`false` se necessário (por padrão vale `true` quando a URL base é https). |
| `HOMEVIEW_ADDR` / `HOMEVIEW_DATA` | `:8080` / `/data` | Endereço de escuta e pasta dos dados (já definidos na imagem). |
| `TZ` | `UTC` | Fuso horário exibido nas telas, ex.: `America/Sao_Paulo`. |

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

### Link interno × externo

Cada serviço pode ter uma **URL interna** (ex.: `http://192.168.1.10:8123`) e uma **URL externa** (ex.: `https://ha.exemplo.com`).

- Clicar no cartão abre, **em uma nova aba**, a URL **externa** quando ela existe; caso contrário, a interna.
- Se o serviço tem as duas, o cartão mostra também a etiqueta **Interno**, que abre a URL interna (também em nova aba).
- O endereço pelo qual você abriu o HomeView não muda nada disso.

> Até a v0.2 o botão principal dependia do endereço de acesso (rede local ou domínio) e existia a variável `HOMEVIEW_INTERNAL_HOSTS`. Ela foi removida; se ainda estiver no seu `.env`, é simplesmente ignorada.

### Catálogo de aplicações

O HomeView conhece **AdGuard Home, Cloudflare DDNS, Cloudflared, Code Server, Debian, Docker, EMQX, ESPHome, FileBrowser, Frigate, go2rtc, Grafana, Home Assistant, Immich, InfluxDB, Jellyfin, Matter Server, MQTT (broker), MQTT Explorer, Music Assistant, Node-RED, Omada Controller, OpenMediaVault, OpenSpeedTest, OpenWrt, Pi-hole, Podman, Portainer, Proxmox, qBittorrent, Tailscale, Ubuntu, UniFi OS, WireGuard (wg-easy) e Zigbee2MQTT**: ícone embutido na imagem (sem CDN, funciona offline) e a porta de acesso usual.

- No formulário de **Serviços**, o campo **Aplicativo conhecido** preenche o ícone (`app:home-assistant`) e, se você já digitou só o IP na URL interna (ex.: `http://192.168.1.10`), completa com a porta padrão (`:8123`).
- Em **Descobertas**, o nome da VM ou do LXC é comparado com o catálogo (`HomeAssistant`, `pihole`, `frigate-nvr`…). Quando reconhece, a URL e o ícone já vêm sugeridos e o item mostra o ícone da aplicação.
- Portas e caminhos são **sugestões** (cada instalação pode ser diferente); confira antes de salvar. Apps sem interface web (broker MQTT, Docker, Debian…) só trazem o ícone.
- Os ícones vêm do projeto [dashboard-icons](https://github.com/homarr-labs/dashboard-icons) (Apache-2.0); as marcas pertencem aos respectivos donos. Veja o arquivo `NOTICE`.

### Descoberta pelo Proxmox

O HomeView lista as VMs (qemu) e os LXCs do Proxmox e deixa você escolher o que entra no painel. Ele só **lê** o Proxmox e **nunca cria nada sozinho**: o que é descoberto fica numa lista de "Novos" até um administrador clicar em **Adicionar ao painel**. O Proxmox não informa a porta dos serviços, então você completa a URL (o IP já vem preenchido quando o Proxmox o conhece).

**1. Crie um token só de leitura no Proxmox** (interface web):

1. *Datacenter → Permissions → Users*: crie `homeview@pve` (realm "Proxmox VE authentication server").
2. *Datacenter → Permissions → API Tokens*: crie o token com ID `dashboard`, mantendo **Privilege Separation** marcado. Copie o segredo, que aparece uma única vez.
3. *Datacenter → Permissions → Add*: dê o papel **PVEAuditor** no caminho `/` para o **token** `homeview@pve!dashboard` e também para o **usuário** `homeview@pve`.

**2. Cadastre a conexão** em **Descobertas → Conexões** (menu do admin): nome, URL (`https://IP:8006`), ID do token (`homeview@pve!dashboard`) e o segredo. O HomeView testa o acesso antes de salvar e não grava nada se falhar.

**3. Aprove o que quiser** em **Descobertas**. Cada item vira um serviço, no grupo que você escolher (por padrão, o nome do nó do Proxmox, criado se não existir). **Uma máquina pode ter vários serviços**: depois da primeira aprovação, use **Adicionar outro serviço**, escolha a aplicação (o IP e a porta usual já vêm preenchidos) e repita quantas vezes precisar. Ex.: um LXC com OpenSpeedTest, Portainer e dois Zigbee2MQTT. A máquina só volta para "Novos" quando o último serviço dela for removido.

Em **Serviços**, o campo **Onde roda** lista as máquinas descobertas e os **hosts do Proxmox**. Escolher um deles preenche o IP na URL interna (junto com a porta da aplicação) e, no caso de uma máquina, mostra o status ligado/desligado no cartão.

Pontos importantes:

- **Certificado**: o Proxmox usa certificado autoassinado por padrão. O jeito seguro é colar a **impressão digital SHA-256** (*Nó → System → Certificates*) no campo próprio: o HomeView passa a confiar só naquele certificado. A opção "não verificar o certificado" existe, mas é último recurso.
- **O segredo do token fica cifrado** no banco (AES-256-GCM, com chave derivada do segredo do servidor, gerado na primeira execução e guardado no volume `/data`). Ele nunca aparece nas telas nem nos logs. Se você perder o volume ou trocar o segredo do servidor, edite a conexão e informe o token de novo.
- **Rede**: o container precisa alcançar `https://IP:8006` do Proxmox (na rede local normalmente funciona sem ajuste).
- **IP do convidado** (best effort): LXC em execução informa o IP pela API; em VM, só com o *QEMU Guest Agent* instalado e habilitado. Sem IP, digite a URL.
- **Sincronização**: automática a cada `HOMEVIEW_DISCOVERY_INTERVAL` e manual pelo botão. O que sumir do Proxmox fica marcado e você decide se esquece; um item volta sozinho se reaparecer. Excluir um serviço devolve o item à lista de "Novos". Itens "Ignorados" podem ser restaurados.
- **Status**: serviços vindos do Proxmox mostram um ponto no cartão (verde = em execução, vazio = parado), conforme a última sincronização.

### Visões do painel

O ícone de camadas, ao lado da engrenagem, troca a forma de ver o painel: **Grupos** (a sua organização, a única em que dá para reorganizar arrastando), **Infraestrutura** (uma seção por VM/LXC descoberta, ordenadas por nó e ID, com ponto de status e IP; serviços sem máquina ficam em *Sem máquina*) e **Tipo** (categoria da aplicação do catálogo; o que não é do catálogo vai para *Outros*). Cada seção também pode ser recolhida.

A tela de login usa o mesmo visual padrão do painel (tema escuro e céu estrelado).

### Montando o painel (admin)

Depois de cadastrar usuários, grupos e serviços (menu ⚙), estas ferramentas ajudam a deixar o painel do seu jeito. Só administradores as veem.

- **Ícone próprio**: em **Serviços** (e ao aprovar uma descoberta) escolha **Ou envie uma imagem do computador** (PNG, JPEG, WebP, GIF ou SVG, até 512 KB). A imagem fica guardada no banco do HomeView, imagens iguais são guardadas uma vez e a imagem é apagada quando nenhum serviço a usa mais.
- **Reorganizar (admin)**: no painel, **Reorganizar** (no menu ⚙) deixa arrastar os cards entre grupos e o ícone ⠿ para mover grupos; também funciona pelo teclado (Alt + ↑/↓). A ordem é salva na hora. O painel usa duas colunas de grupos em telas largas (e uma em telas estreitas), e cada grupo mostra no máximo duas colunas de cards.
- **Editor YAML (admin)**: no menu ⚙ → **Editar YAML**. Grupos e serviços aparecem como texto; **Validar** mostra o que muda e **Aplicar** grava (o que não estiver no arquivo é removido). Dá para baixar `homeview.yml`. Conexões Proxmox, usuários e a descoberta não entram. A configuração fica no SQLite `homeview.db`, no volume Docker `homeview-data` (`/data`).

### Aparência e uso (por usuário)

No menu ⚙ → **Aparência**, cada pessoa escolhe, só para a sua conta e valendo em qualquer aparelho:

- **Tema**: automático (segue o aparelho), claro ou escuro.
- **Cor de destaque**: oito cores prontas ou qualquer outra (o texto sobre ela ajusta para branco ou escuro, mantendo o contraste).
- **Menu em engrenagem**: Serviços, Grupos, Editar YAML, Descobertas, Usuários, Aparência, Meus dispositivos e Sair ficam num menu suspenso no ícone ⚙ do cabeçalho (fecha com Esc ou clicando fora).
- **Recolher grupos**: clique no nome do grupo para esconder os cards e ganhar espaço. A escolha fica salva neste navegador e a busca expande os grupos automaticamente.
- **Fundos prontos**: 11 imagens originais (Céu estrelado é o padrão, junto com o tema escuro; há também a opção "Sem fundo"). Gerar de novo: `python3 tools/backgrounds/gen.py <pasta>` (precisa de Pillow e numpy).
- **Imagem de fundo própria**: PNG, JPEG ou WebP de até 3 MB, com controle de esmaecimento para manter o texto legível. A imagem fica no banco (volume `/data`), só é entregue ao dono e SVG não é aceito.

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
