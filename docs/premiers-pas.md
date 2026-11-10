# Premiers pas avec WoL Manager

Ce guide accompagne une première installation Linux. Il ne remplace pas la
[référence CLI](cli.md).

## Ce que fait WoL Manager

WoL Manager envoie un paquet Wake-on-LAN à une machine **déjà configurée**.
Pour que celle-ci démarre, son BIOS/UEFI, sa carte réseau, son alimentation et
le réseau doivent autoriser le réveil. Un paquet accepté par le système n'est
pas une preuve de réception ni de démarrage.

## Parcours conseillé

1. Suivez le [guide d'installation](installation.md).
2. Ouvrez l'assistant avec `sudo wol setup` et créez un réseau puis une
   machine avec son adresse MAC, son IP et son réseau.
3. Validez avec `wol config check` après avoir ouvert une nouvelle session ou
   exécuté `newgrp wol-manager` comme indiqué par l'installateur.
4. Consultez les machines avec `wol list`, puis testez
   `wol status IDENTIFIANT`.
5. Simulez sans paquet avec `wol wake IDENTIFIANT --dry-run`.
6. Déclenchez un réveil réel avec `wol wake IDENTIFIANT` uniquement après
   avoir relu le plan, puis vérifiez l'état réel de la machine.
7. Créez une première sauvegarde avec `sudo wol backup create`, puis vérifiez-la
   avec `sudo wol backup list` et `sudo wol backup verify FICHIER`.
8. Ajoutez ensuite, si nécessaire, des [groupes et dépendances](configuration.md),
   une protection [UPS NUT](nut.md) ou l'[API REST](api.md).

`IDENTIFIANT` est le nom technique enregistré par l'assistant, pas l'adresse
IP. Les commandes d'écriture de configuration et de gestion des jetons
restent administratives et s'exécutent avec `sudo`.

## Trois notions essentielles

- **Paquet WoL :** demande réseau sans accusé de réception de la machine
  éteinte.
- **Contrôle de disponibilité :** test ICMP, TCP ou ARP après le réveil ; son
  résultat dépend du réseau et des contrôles configurés.
- **Réveil vérifié :** résultat positif de ces contrôles, distinct du succès
  d'écriture dans une socket UDP.

## Si rien ne démarre

Vérifiez dans cet ordre : alimentation, activation WoL dans le BIOS/UEFI,
carte réseau alimentée en veille, adresse MAC, réseau et transport. Poursuivez
avec le [diagnostic guidé](diagnostics.md). Une interface USB ou certains états
d'extinction peuvent ne pas accepter WoL même si Linux indique `Wake-on: g`.

## Pour aller plus loin

- [Configuration](configuration.md) et [réseaux/VLAN](multi-vlan.md) ;
- [UPS NUT](nut.md), [sauvegardes](backups.md), [mises à jour](updates.md) et [sécurité](security.md) ;
- [API REST](api.md) et [référence CLI](cli.md).
