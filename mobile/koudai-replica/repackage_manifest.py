"""Build a co-installable replica with new manifest and resource-package identity.

Does not execute APK code, install anything, access an account, or contact a server.
Original SDK keys, native implementations and network endpoints are not changed.
"""
import argparse
import copy
import hashlib
import json
import pathlib
import re
import struct
import zipfile

ROOT = pathlib.Path(__file__).resolve().parent
OUT = ROOT / 'work'
SOURCE = ROOT.parents[1] / 'reports/source-probes/koudai-mobile-20261119/base.apk'
TARGET = OUT / 'koudai-replica-unsigned.apk'
OLD = 'com.niudian.yun.cordova.koudai'
NEW = OLD + '.replica'
LABEL = '\u53e3\u888b\u725b\u5e97\u00b7\u72ec\u7acb\u6d4b\u8bd5'
SCHEME = 'koudai-replica'
NONE = 0xffffffff
ANDROID = 'http://schemas.android.com/apk/res/android'


def chunks(data):
    kind, header, size = struct.unpack_from('<HHI', data)
    assert kind == 3 and header == 8 and size == len(data)
    offset = header
    while offset < size:
        kind, header, length = struct.unpack_from('<HHI', data, offset)
        assert length >= header >= 8 and offset + length <= size
        yield offset, kind, header, data[offset:offset + length]
        offset += length
    assert offset == size


def read_length(data, offset, wide):
    if wide:
        first = struct.unpack_from('<H', data, offset)[0]
        if first & 0x8000:
            return ((first & 0x7fff) << 16) | struct.unpack_from('<H', data, offset + 2)[0], offset + 4
        return first, offset + 2
    first = data[offset]
    if first & 0x80:
        return ((first & 0x7f) << 8) | data[offset + 1], offset + 2
    return first, offset + 1


def write_length(value, wide):
    if wide:
        return struct.pack('<H', value) if value < 0x8000 else struct.pack('<HH', 0x8000 | (value >> 16), value & 0xffff)
    assert value < 0x8000
    return bytes([value]) if value < 0x80 else bytes([0x80 | (value >> 8), value & 0xff])


class StringPool:
    def __init__(self, data):
        self.original = data
        _, self.header_size, _ = struct.unpack_from('<HHI', data)
        self.count, self.styles, self.flags, self.start, self.styles_start = struct.unpack_from('<IIIII', data, 8)
        self.strings = []
        self.added = []
        self.offsets = [struct.unpack_from('<I', data, self.header_size + 4 * i)[0] for i in range(self.count)]
        for relative in self.offsets:
            offset = self.start + relative
            units, offset = read_length(data, offset, not self.flags & 256)
            if self.flags & 256:
                length, offset = read_length(data, offset, False)
                value = data[offset:offset + length].decode('utf-8')
            else:
                value = data[offset:offset + units * 2].decode('utf-16le')
            self.strings.append(value)

    def index(self, value):
        if value in self.strings:
            return self.strings.index(value)
        self.added.append(value)
        self.strings.append(value)
        return len(self.strings) - 1

    def build(self):
        if not self.added:
            return self.original
        old = self.original
        end = self.styles_start or len(old)
        blob = bytearray(old[self.start:end])
        added_offsets = []
        for text in self.added:
            added_offsets.append(len(blob))
            encoded16 = text.encode('utf-16le')
            if self.flags & 256:
                encoded = text.encode('utf-8')
                blob += write_length(len(encoded16) // 2, False) + write_length(len(encoded), False) + encoded + b'\0'
            else:
                blob += write_length(len(encoded16) // 2, True) + encoded16 + b'\0\0'
        blob += b'\0' * (-len(blob) % 4)
        extra_offsets = b''.join(struct.pack('<I', x) for x in added_offsets)
        offset_table = old[self.header_size:self.header_size + 4 * self.count] + extra_offsets
        offset_table += old[self.header_size + 4 * self.count:self.start]
        new_start = self.start + 4 * len(self.added)
        style_blob = old[end:] if self.styles_start else b''
        header = bytearray(old[:self.header_size])
        struct.pack_into('<I', header, 4, new_start + len(blob) + len(style_blob))
        struct.pack_into('<I', header, 8, len(self.strings))
        struct.pack_into('<I', header, 16, self.flags & ~1)  # appended strings are not sorted
        struct.pack_into('<I', header, 20, new_start)
        struct.pack_into('<I', header, 24, new_start + len(blob) if self.styles_start else 0)
        return bytes(header) + offset_table + blob + style_blob


def elements(data):
    parts = list(chunks(data))
    pool = StringPool(next(raw for _, kind, _, raw in parts if kind == 1))
    stack, result = [], []
    for offset, kind, _, raw in parts:
        if kind == 0x102:
            tag = pool.strings[struct.unpack_from('<I', raw, 20)[0]]
            attr_start, attr_size, count = struct.unpack_from('<HHH', raw, 24)
            assert attr_size == 20
            attrs = {}
            for i in range(count):
                position = 16 + attr_start + i * attr_size
                ns, name, raw_index = struct.unpack_from('<III', raw, position)
                value_type = raw[position + 15]
                value_data = struct.unpack_from('<I', raw, position + 16)[0]
                value = pool.strings[value_data] if value_type == 3 else value_data
                attrs[pool.strings[name]] = {'namespace': None if ns == NONE else pool.strings[ns], 'position': position,
                                             'type': value_type, 'value': value, 'rawIndex': raw_index}
            element = {'tag': tag, 'attrs': attrs, 'offset': offset, 'parent': stack[-1] if stack else None, 'raw': raw}
            result.append(element)
            stack.append(element)
        elif kind == 0x103:
            stack.pop()
    assert not stack
    return parts, pool, result


def val(element, attr):
    return element['attrs'].get(attr, {}).get('value')


def qualified_class(value):
    if not isinstance(value, str) or not value:
        return value
    return OLD + value if value.startswith('.') else OLD + '.' + value if '.' not in value else value


def patch_manifest(original):
    parts, pool, nodes = elements(original)
    assert pool.build() == pool.original
    manifest = next(n for n in nodes if n['tag'] == 'manifest')
    assert val(manifest, 'package') == OLD
    assert 'sharedUserId' not in manifest['attrs'], 'sharedUserId needs separate design'
    launchers = []
    for node in nodes:
        if node['tag'] != 'intent-filter':
            continue
        children = [n for n in nodes if n['parent'] is node]
        actions = [val(n, 'name') for n in children if n['tag'] == 'action']
        categories = [val(n, 'name') for n in children if n['tag'] == 'category']
        if 'android.intent.action.MAIN' in actions and 'android.intent.category.LAUNCHER' in categories:
            launchers.append(node['parent'])
    assert len(launchers) == 1
    edited, changes = {}, []

    def replace(node, name, value, reason):
        attr = node['attrs'][name]
        if attr['type'] == 3 and attr['value'] == value:
            return
        raw = edited.setdefault(node['offset'], bytearray(node['raw']))
        index = pool.index(value)
        pos = attr['position']
        struct.pack_into('<I', raw, pos + 8, index)
        raw[pos + 15] = 3
        struct.pack_into('<I', raw, pos + 16, index)
        changes.append({'tag': node['tag'], 'attribute': name, 'old': attr['value'], 'new': value, 'reason': reason})

    replace(manifest, 'package', NEW, 'independent install identity')
    for node in nodes:
        tag, attrs = node['tag'], node['attrs']
        if tag == 'application' or any(node is l for l in launchers) or (tag == 'intent-filter' and any(node['parent'] is l for l in launchers)):
            if 'label' in attrs:
                replace(node, 'label', LABEL, 'independent application / launcher identity')
        # App namespace and its Java packages need not match: preserve all classes.
        if tag in {'application', 'activity', 'activity-alias', 'service', 'receiver', 'provider', 'instrumentation'}:
            for name in ('name', 'targetActivity', 'parentActivityName', 'backupAgent', 'appComponentFactory'):
                if name in attrs and isinstance(val(node, name), str):
                    replace(node, name, qualified_class(val(node, name)), 'preserve original absolute component class')
        for name in ('authorities', 'permission', 'readPermission', 'writePermission', 'taskAffinity', 'process'):
            value = val(node, name)
            if isinstance(value, str) and OLD in value:
                replace(node, name, value.replace(OLD, NEW), 'isolate provider / permission / task / process identity')
        if tag in {'permission', 'permission-tree', 'permission-group', 'uses-permission', 'uses-permission-sdk-23'}:
            value = val(node, 'name')
            if isinstance(value, str) and value.startswith(OLD + '.'):
                replace(node, 'name', NEW + value[len(OLD):], 'isolate own custom permission')
        if tag == 'data' and val(node, 'scheme') == 'koudai':
            replace(node, 'scheme', SCHEME, 'isolate custom deep-link scheme')
        if tag == 'meta-data' and val(node, 'name') == 'URL_SCHEME' and val(node, 'value') == 'koudai':
            replace(node, 'value', SCHEME, 'keep declared URL_SCHEME in sync')
    payload = b''.join(pool.build() if kind == 1 else bytes(edited.get(offset, raw)) for offset, kind, _, raw in parts)
    patched = struct.pack('<HHI', 3, 8, len(payload) + 8) + payload
    _, parsed_pool, checked = elements(patched)
    assert parsed_pool.strings == pool.strings
    assert len(checked) == len(nodes)
    assert next(val(n, 'package') for n in checked if n['tag'] == 'manifest') == NEW
    changed_attributes = 0
    for before, after in zip(nodes, checked):
        assert before['tag'] == after['tag'] and before['attrs'].keys() == after['attrs'].keys()
        for name in before['attrs']:
            a, b = before['attrs'][name], after['attrs'][name]
            assert a['namespace'] == b['namespace']
            if (a['type'], a['value']) != (b['type'], b['value']):
                changed_attributes += 1
        if before['tag'] in {'application', 'activity', 'activity-alias', 'service', 'receiver', 'provider', 'instrumentation'}:
            if 'name' in before['attrs']:
                assert qualified_class(val(before, 'name')) == val(after, 'name')
    assert changed_attributes == len(changes)
    for node in checked:
        for name in ('authorities', 'permission', 'readPermission', 'writePermission', 'taskAffinity', 'process'):
            value = val(node, name)
            assert not (isinstance(value, str) and OLD in value and NEW not in value)
    before_authorities = {val(n, 'authorities') for n in nodes if n['tag'] == 'provider'}
    after_authorities = {val(n, 'authorities') for n in checked if n['tag'] == 'provider'}
    assert not before_authorities & after_authorities
    report = {'oldPackage': OLD, 'newPackage': NEW, 'label': LABEL, 'newCustomScheme': SCHEME,
              'originalManifestBytes': len(original), 'patchedManifestBytes': len(patched),
              'stringCountBefore': pool.count, 'stringCountAfter': len(pool.strings),
              'elementCount': len(nodes), 'changedAttributeCount': changed_attributes,
              'providerDeclarations': sum(n['tag'] == 'provider' for n in nodes),
              'uniqueProviderAuthoritiesBefore': len(before_authorities), 'uniqueProviderAuthoritiesAfter': len(after_authorities),
              'providerAuthorityIntersection': [], 'launcherClass': qualified_class(val(launchers[0], 'name')),
              'taskAffinity': 'No explicit taskAffinity in source; Android default derives from new application package.',
              'changes': changes,
              'limitations': ['No runtime or install tested.', 'DEX still contains any original hardcoded identity or authority strings.',
                              'Original SDK credentials are preserved; new package/signature may not be authorized by those services.',
                              'Original duplicate provider authorities remain duplicates inside replica; this matches source semantics.',
                              'Original custom scheme is isolated, so external callbacks to koudai will still open the original app.',
                              'Only the resource package-name header follows the new app ID; resource IDs, values and component classes are unchanged.']}
    return patched, report


def hash_bytes(value):
    return hashlib.sha256(value).hexdigest()


def patch_resource_package_name(original):
    kind, header, length, count = struct.unpack_from('<HHII', original)
    assert kind == 2 and length == len(original) and count == 1
    output = bytearray(original)
    offset, patches = header, []
    while offset < length:
        kind, chunk_header, chunk_size = struct.unpack_from('<HHI', original, offset)
        assert chunk_size >= chunk_header >= 8 and offset + chunk_size <= length
        if kind == 0x200:
            assert chunk_header >= 284
            package_id = struct.unpack_from('<I', original, offset + 8)[0]
            start, end = offset + 12, offset + 268
            name = original[start:end].decode('utf-16le').split('\0', 1)[0]
            assert package_id == 0x7f and name == OLD
            encoded = NEW.encode('utf-16le') + b'\0\0'
            assert len(encoded) <= 256
            output[start:end] = encoded.ljust(256, b'\0')
            patches.append({'chunkOffset': offset, 'packageId': package_id, 'byteOffset': start,
                            'byteLength': 256, 'byteEndExclusive': end, 'oldName': OLD, 'newName': NEW,
                            'reason': 'Cordova config lookup falls back to context.getPackageName()'})
        offset += chunk_size
    assert offset == length and len(patches) == 1 and len(output) == len(original)
    masked_before, masked_after = bytearray(original), bytearray(output)
    for patch in patches:
        start, end = patch['byteOffset'], patch['byteEndExclusive']
        masked_before[start:end] = b'\0' * 256
        masked_after[start:end] = b'\0' * 256
    assert masked_before == masked_after
    return bytes(output), {'patches': patches, 'fileSizeUnchanged': len(output),
                           'outsideHeaderSha256Before': hash_bytes(masked_before),
                           'outsideHeaderSha256After': hash_bytes(masked_after)}


def main():
    global SOURCE, OUT, TARGET
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=pathlib.Path, default=SOURCE)
    parser.add_argument('--work', type=pathlib.Path, default=OUT)
    args = parser.parse_args()
    SOURCE = args.source.resolve(strict=True)
    OUT = args.work.resolve()
    TARGET = OUT / 'koudai-replica-unsigned.apk'
    if SOURCE == TARGET:
        parser.error('Source APK must not be the build output')
    OUT.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(SOURCE) as source:
        names = source.namelist()
        assert len(set(names)) == len(names), 'duplicate ZIP members need separate handling'
        original = source.read('AndroidManifest.xml')
        patched, report = patch_manifest(original)
        patched_resources, resource_report = patch_resource_package_name(source.read('resources.arsc'))
        report['resourcePackageNamePatch'] = resource_report
        (OUT / 'AndroidManifest-original.axml').write_bytes(original)
        (OUT / 'AndroidManifest-replica.axml').write_bytes(patched)
        removed, records = [], []
        with zipfile.ZipFile(TARGET, 'w', allowZip64=True) as target:
            target.comment = source.comment
            for item in source.infolist():
                if re.fullmatch(r'META-INF/(?:MANIFEST\.MF|[^/]+\.(?:SF|RSA|DSA|EC))', item.filename, re.I):
                    removed.append(item.filename)
                    continue
                payload = source.read(item.filename)
                new_payload = patched if item.filename == 'AndroidManifest.xml' else patched_resources if item.filename == 'resources.arsc' else payload
                target.writestr(copy.copy(item), new_payload)
                records.append({'name': item.filename, 'sha256Before': hash_bytes(payload), 'sha256After': hash_bytes(new_payload),
                                'unchangedPayload': payload == new_payload, 'uncompressedBytes': len(new_payload), 'compression': item.compress_type})
    with zipfile.ZipFile(TARGET) as result:
        assert result.testzip() is None
        for record in records:
            assert hash_bytes(result.read(record['name'])) == record['sha256After']
    changed = [r['name'] for r in records if not r['unchangedPayload']]
    assert set(changed) == {'AndroidManifest.xml', 'resources.arsc'}
    report.update({'sourceApkSha256': hash_bytes(SOURCE.read_bytes()), 'outputApkSha256': hash_bytes(TARGET.read_bytes()),
                   'sourceApk': str(SOURCE), 'unsignedApk': str(TARGET), 'keptZipEntries': len(records),
                   'changedEntryPayloads': changed, 'removedObsoleteV1Signatures': removed,
                   'v2V3SigningBlock': 'not copied by ZIP rewrite; this output is deliberately unsigned',
                   'bytePreservationScope': 'All retained entry payloads unchanged except Manifest and the fixed 256-byte ARSC package-name field. ZIP compressed streams and offsets may differ.'})
    (OUT / 'entry-sha256-verification.json').write_text(json.dumps(records, ensure_ascii=False, indent=2), encoding='utf-8')
    (OUT / 'prototype-report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps({k: report[k] for k in ['newPackage', 'changedAttributeCount', 'keptZipEntries', 'changedEntryPayloads', 'unsignedApk']}, ensure_ascii=True))


if __name__ == '__main__':
    main()
