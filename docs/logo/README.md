# LeaSSH, logos PNG

Ces fichiers sont extraits directement de la planche d'origine (`source/planche-originale.png`), sans redessin. Chaque élément a été découpé, agrandi quatre fois avec un réseau de super-résolution (EDSR x4) puis détouré. Seules les baselines françaises ont été ajoutées : le texte anglais a été effacé et remplacé par le texte français en Figtree Regular, la police la plus proche de celle de la planche, à la même taille, à la même couleur et à la même position, avec le même point médian rond.

## Contenu

```
logos/
  leassh-logo-clair-{en,fr}-transparent.png     fond transparent, pour fonds clairs
  leassh-logo-clair-{en,fr}-fond-blanc.png      sur fond blanc
  leassh-logo-sombre-{en,fr}.png                bandeau marine aux angles arrondis
  leassh-logo-sombre-{en,fr}-transparent.png    texte blanc, fond transparent, pour fonds sombres
  leassh-logo-monochrome-{en,fr}-transparent.png
  leassh-logo-monochrome-{en,fr}-fond-blanc.png
  leassh-logo-contour-{en,fr}-transparent.png
  leassh-logo-contour-{en,fr}-fond-blanc.png
  leassh-nom-seul-transparent.png               le mot LeaSSH sans symbole ni baseline
symboles/
  leassh-symbole-couleur-transparent.png
  leassh-symbole-couleur-fond-blanc.png
  leassh-symbole-monochrome-transparent.png
  leassh-symbole-contour-transparent.png
icone/
  leassh-icone-{16,32,48,64,128,180,192,256,512,1024}.png   coins arrondis transparents
favicon/
  favicon.ico (16, 32, 48, 64), favicon-16x16.png, favicon-32x32.png,
  apple-touch-icon.png, android-chrome-192x192.png, android-chrome-512x512.png,
  site.webmanifest
source/
  planche-originale.png
```

Intégration HTML :

```html
<link rel="icon" href="/favicon.ico" sizes="any">
<link rel="icon" type="image/png" sizes="32x32" href="/favicon-32x32.png">
<link rel="apple-touch-icon" href="/apple-touch-icon.png">
<link rel="manifest" href="/site.webmanifest">
```

## Limites

La planche fait 1536 x 1024 pixels et le logo principal n'y occupe qu'environ 880 x 330 pixels. Après agrandissement x4, le logo clair fait environ 3600 pixels de large : c'est net à l'écran et pour un usage web ou une présentation, mais ce n'est pas un original haute définition. Pour une impression grand format, il faudra régénérer l'image source en plus grande taille.

L'icône 16 x 16 est l'icône complète réduite, comme sur la planche : à cette taille seuls la silhouette, la visière et la pastille violette restent lisibles.
