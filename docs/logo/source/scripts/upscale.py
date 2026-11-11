import cv2, numpy as np, sys
sr=cv2.dnn_superres.DnnSuperResImpl_create(); sr.readModel('models/EDSR_x4.pb'); sr.setModel('edsr',4)
im=cv2.imread('src/ref.png'); H,W=im.shape[:2]
out=np.zeros((H*4,W*4,3),np.uint8)
T=256; O=16
tiles=[(x,y) for y in range(0,H,T) for x in range(0,W,T)]
for i,(x,y) in enumerate(tiles):
    x0,y0=max(0,x-O),max(0,y-O); x1,y1=min(W,x+T+O),min(H,y+T+O)
    tile=im[y0:y1,x0:x1]
    if tile.min()>245: up=cv2.resize(tile,None,fx=4,fy=4,interpolation=cv2.INTER_CUBIC)
    else: up=sr.upsample(tile)
    cx0,cy0=(x-x0)*4,(y-y0)*4; w=min(T,W-x)*4; h=min(T,H-y)*4
    out[y*4:y*4+h,x*4:x*4+w]=up[cy0:cy0+h,cx0:cx0+w]
    print(i+1,len(tiles),flush=True)
cv2.imwrite('work/ref_x4.png',out)
print('done')
