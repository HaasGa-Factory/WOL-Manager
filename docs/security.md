# Sécurité

- API distante exclusivement en HTTPS ; relais exclusivement en HTTPS mTLS.
- Jetons API aléatoires de 256 bits, hash SHA-256 stocké, comparaison constante.
- Permissions minimales `read`, `wake`, `admin` et limitation par jeton/adresse.
- Corps HTTP limités, JSON strict, délais réseau et en-têtes défensifs.
- Aucune destination réseau libre dans l'API ou le protocole relais.
- Broadcast dirigé et découverte active refusés par défaut.
- Sous-réseaux publics interdits pour la découverte active.
- Audit sans jetons, mots de passe ni clés.
- Commandes système limitées à `ip`, `ping` et l'éditeur local, avec arguments construits par le programme.

Le compte systemd utilise des restrictions de fichiers, processus et privilèges. L'accès à des interfaces particulières peut nécessiter une adaptation locale soigneusement documentée.

Signaler les vulnérabilités selon [`SECURITY.md`](../SECURITY.md), jamais dans une issue publique avant coordination.
