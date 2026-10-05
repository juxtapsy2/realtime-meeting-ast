// Sealed-box transport for admin bodies that carry provider API keys.
//
// The backend publishes the public half of an ECDH P-256 key pair at
// /api/sealedbox/public-key. Before sending an update that contains a secret,
// we generate a one-shot key pair for this request, agree with the server's
// public key, derive a content-encryption key with HKDF-SHA256, and AES-256-GCM
// encrypt the JSON body. The envelope replaces the body; only the backend holds
// the private key that can read it.
//
// This is defence in depth on top of TLS: it keeps secrets out of plaintext
// HTTP bodies on the server side (body loggers, proxies, middleware), and it
// is deliberately best-effort. Two situations cannot seal:
//
//   - an insecure context, where window.crypto.subtle is absent, and
//   - a server that publishes an algorithm this build does not recognise.
//
// In both cases we fall back to sending plain JSON so saving still works, and
// we say so in the console. The backend accepts either form.
const ALGORITHM = 'ECDH-P256+HKDF-SHA256+AES-256-GCM';
const INFO_PREFIX = 'realtime-meeting-ast:sealbox:v1:';
const VERSION = 1;
const KEY_BITS = 256;
const NONCE_SIZE = 12;
const POINT_SIZE = 65; // uncompressed P-256 point

type ServerKey = { alg: string; kid: string; pub: string };
type Envelope = { v: number; kid: string; epk: string; n: string; ct: string };

const encoder = new TextEncoder();

export function sealboxAvailable(): boolean {
  return typeof crypto !== 'undefined' && typeof crypto.subtle !== 'undefined';
}

function toBase64(bytes: Uint8Array): string {
  let binary = '';
  for (let i = 0; i < bytes.length; i++) {
    binary += String.fromCharCode(bytes[i]);
  }
  return btoa(binary);
}

// Web Crypto only accepts buffers backed by ArrayBuffer, so this allocates one
// explicitly rather than the ArrayBufferLike a plain Uint8Array may reference.
function fromBase64(value: string): Uint8Array<ArrayBuffer> {
  const binary = atob(value);
  const bytes = new Uint8Array(new ArrayBuffer(binary.length));
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

async function fetchServerKey(): Promise<ServerKey> {
  const response = await fetch('/api/sealedbox/public-key');
  if (!response.ok) {
    throw new Error(`seal box public key unavailable (status ${response.status})`);
  }
  const key = (await response.json()) as ServerKey;
  if (key.alg !== ALGORITHM) {
    throw new Error(`unsupported seal box algorithm ${key.alg}`);
  }
  const point = fromBase64(key.pub);
  if (point.length !== POINT_SIZE || point[0] !== 0x04) {
    throw new Error('seal box public key is not an uncompressed P-256 point');
  }
  return key;
}

// seal encrypts one payload for the published server key. HKDF must match the
// backend exactly: the salt is the published point, and the context binds the
// key id together with this request's own ephemeral point.
async function seal(server: ServerKey, payload: unknown): Promise<Envelope> {
  const serverPoint = fromBase64(server.pub);

  const ephemeral = (await crypto.subtle.generateKey(
    { name: 'ECDH', namedCurve: 'P-256' },
    true,
    ['deriveBits'],
  )) as CryptoKeyPair;
  const ephemeralPoint = new Uint8Array(
    await crypto.subtle.exportKey('raw', ephemeral.publicKey),
  );
  const epk = toBase64(ephemeralPoint);

  // The server's point is only ever handed to deriveBits as the peer key, so
  // it carries no usages of its own: an imported ECDH public key must be
  // created with an empty usage list. Chrome and Node both reject
  // deriveBits/deriveKey here, so a non-empty list breaks sealing outright.
  const peer = await crypto.subtle.importKey(
    'raw',
    serverPoint,
    { name: 'ECDH', namedCurve: 'P-256' },
    false,
    [],
  );
  const shared = new Uint8Array(
    await crypto.subtle.deriveBits({ name: 'ECDH', public: peer }, ephemeral.privateKey, KEY_BITS),
  );

  const material = await crypto.subtle.importKey('raw', shared, 'HKDF', false, ['deriveKey']);
  const key = await crypto.subtle.deriveKey(
    {
      name: 'HKDF',
      hash: 'SHA-256',
      salt: serverPoint,
      info: encoder.encode(`${INFO_PREFIX}${server.kid}:${epk}`),
    },
    material,
    { name: 'AES-GCM', length: KEY_BITS },
    false,
    ['encrypt'],
  );

  const nonce = crypto.getRandomValues(new Uint8Array(NONCE_SIZE));
  const ciphertext = new Uint8Array(
    await crypto.subtle.encrypt(
      { name: 'AES-GCM', iv: nonce },
      key,
      encoder.encode(JSON.stringify(payload)),
    ),
  );

  return {
    v: VERSION,
    kid: server.kid,
    epk,
    n: toBase64(nonce),
    ct: toBase64(ciphertext),
  };
}

/**
 * Returns the JSON body to send for an admin update. Secrets are sealed when
 * the environment allows it; otherwise the plain JSON is returned unchanged so
 * the save still succeeds.
 */
export async function sealJSON(payload: unknown): Promise<string> {
  const plaintext = JSON.stringify(payload);

  if (!sealboxAvailable()) {
    console.warn(
      '[sealedbox] Web Crypto is unavailable (insecure context); sending the API key in plaintext JSON',
    );
    return plaintext;
  }

  try {
    const server = await fetchServerKey();
    return JSON.stringify({ sealed: await seal(server, payload) });
  } catch (error) {
    console.warn('[sealedbox] could not seal this request; sending plaintext JSON', error);
    return plaintext;
  }
}
