"""Offline diagnostic APK: add only android:debuggable=true to the replica.

No installation, device interaction, signing, accounts, network, DEX changes,
certificate changes, or API/entitlement changes. Existing signature files are
removed because the output is unsigned and must be signed by the root workflow.
"""
import argparse
import copy
import hashlib
import json
import pathlib
import re
import struct
import zipfile
import repackage_manifest as editor

HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parents[1]
DEBUGGABLE_ID = 0x0101000f
NONE = 0xffffffff


def add_debuggable(original):
    """Append the string/map entry, insert a sorted framework bool attribute."""
    parts, pool, nodes = editor.elements(original)
    identity = next(editor.val(node, 'package') for node in nodes if node['tag'] == 'manifest')
    assert identity == editor.NEW, 'Only the separate replica application may be made diagnostic.'
    application = next(n for n in nodes if n['tag'] == 'application')
    assert 'debuggable' not in application['attrs'], 'Prototype expects absent attribute; do not silently override it.'
    before_strings = pool.strings.copy()
    ns_index = pool.index(editor.ANDROID)
    name_index = pool.index('debuggable')
    maps = [(offset, header, raw) for offset, kind, header, raw in parts if kind == 0x180]
    assert len(maps) == 1
    map_offset, map_header, old_map = maps[0]
    assert (len(old_map) - map_header) % 4 == 0
    old_ids = list(struct.unpack_from('<' + 'I' * ((len(old_map) - map_header) // 4), old_map, map_header))
    ids = old_ids.copy()
    if name_index >= len(ids):
        ids += [0] * (name_index + 1 - len(ids))
    assert ids[name_index] in (0, DEBUGGABLE_ID)
    ids[name_index] = DEBUGGABLE_ID
    resource_map = bytearray(old_map[:map_header])
    resource_map += struct.pack('<' + 'I' * len(ids), *ids)
    struct.pack_into('<I', resource_map, 4, len(resource_map))

    raw = application['raw']
    attribute_start, attribute_size, attribute_count = struct.unpack_from('<HHH', raw, 24)
    assert attribute_size == 20
    array_start = 16 + attribute_start
    attributes = [raw[array_start + i * 20:array_start + (i + 1) * 20] for i in range(attribute_count)]
    # Android framework attrs are in resource-ID order. Preserve that ordering.
    insertion = attribute_count
    for index, attribute in enumerate(attributes):
        existing_name = struct.unpack_from('<I', attribute, 4)[0]
        existing_id = old_ids[existing_name] if existing_name < len(old_ids) else 0
        if existing_id > DEBUGGABLE_ID:
            insertion = index
            break
    # ResXMLTree_attribute + Res_value: namespace,name,raw,size,res0,type,data.
    new_attribute = struct.pack('<IIIHBBI', ns_index, name_index, NONE, 8, 0, 0x12, 0xffffffff)
    attributes.insert(insertion, new_attribute)
    changed_node = bytearray(raw[:array_start])
    changed_node += b''.join(attributes) + raw[array_start + attribute_count * 20:]
    struct.pack_into('<I', changed_node, 4, len(changed_node))
    struct.pack_into('<H', changed_node, 28, attribute_count + 1)
    # idIndex/classIndex/styleIndex are one-based attribute positions (0=absent).
    for field_offset in (30, 32, 34):
        value = struct.unpack_from('<H', raw, field_offset)[0]
        if value and value >= insertion + 1:
            struct.pack_into('<H', changed_node, field_offset, value + 1)
    payload = b''.join(pool.build() if kind == 1 else bytes(resource_map) if offset == map_offset else
                       bytes(changed_node) if offset == application['offset'] else chunk
                       for offset, kind, _, chunk in parts)
    patched = struct.pack('<HHI', 3, 8, len(payload) + 8) + payload

    checked_parts, checked_pool, checked_nodes = editor.elements(patched)
    checked_app = next(n for n in checked_nodes if n['tag'] == 'application')
    debug = checked_app['attrs']['debuggable']
    assert debug['namespace'] == editor.ANDROID and debug['type'] == 0x12 and debug['value'] == 0xffffffff
    assert checked_pool.strings[:len(before_strings)] == before_strings
    assert checked_pool.strings[name_index] == 'debuggable'
    assert ids[:len(old_ids)] == old_ids
    assert ids[name_index] == DEBUGGABLE_ID
    assert len(nodes) == len(checked_nodes) and len(parts) == len(checked_parts)
    for before, after in zip(nodes, checked_nodes):
        assert before['tag'] == after['tag']
        extra = {'debuggable'} if before is application else set()
        assert set(after['attrs']) == set(before['attrs']) | extra
        for name, attr in before['attrs'].items():
            observed = after['attrs'][name]
            assert (attr['namespace'], attr['type'], attr['value']) == (observed['namespace'], observed['type'], observed['value'])
    for before, after in zip(parts, checked_parts):
        assert before[1] == after[1]
        if before[1] not in (1, 0x180) and before[0] != application['offset']:
            assert before[3] == after[3]
    return patched, {'addedAttribute': 'android:debuggable', 'resourceId': hex(DEBUGGABLE_ID),
                     'typedValueType': 'TYPE_INT_BOOLEAN (0x12)', 'typedValue': '0xffffffff',
                     'oldStringCount': len(before_strings), 'newStringCount': len(pool.strings),
                     'debuggableStringIndex': name_index, 'oldResourceMapEntries': len(old_ids),
                     'newResourceMapEntries': len(ids), 'applicationAttributeInsertionIndexZeroBased': insertion,
                     'allExistingAttributeSemanticsUnchanged': True, 'allOtherXmlNodesUnchanged': True,
                     'beforeManifestBytes': len(original), 'afterManifestBytes': len(patched)}


def sha(data):
    return hashlib.sha256(data).hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--source', type=pathlib.Path, default=ROOT / 'mobile/koudai-replica/out/koudai-replica-1.1.79.apk')
    parser.add_argument('--out', type=pathlib.Path, default=HERE / 'work/koudai-replica-debuggable-unsigned.apk')
    options = parser.parse_args()
    assert options.source.resolve() != options.out.resolve()
    options.out.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(options.source) as source:
        original = source.read('AndroidManifest.xml')
        patched, report = add_debuggable(original)
        (options.out.parent / 'AndroidManifest-debuggable.axml').write_bytes(patched)
        kept, removed = [], []
        with zipfile.ZipFile(options.out, 'w', allowZip64=True) as target:
            for item in source.infolist():
                if re.fullmatch(r'META-INF/(?:MANIFEST\.MF|[^/]+\.(?:SF|RSA|DSA|EC))', item.filename, re.I):
                    removed.append(item.filename)
                    continue
                data = source.read(item.filename)
                result = patched if item.filename == 'AndroidManifest.xml' else data
                target.writestr(copy.copy(item), result)
                kept.append({'name': item.filename, 'before': sha(data), 'after': sha(result)})
    with zipfile.ZipFile(options.out) as result:
        assert result.testzip() is None
        assert all(sha(result.read(item['name'])) == item['after'] for item in kept)
    changed = [item['name'] for item in kept if item['before'] != item['after']]
    assert changed == ['AndroidManifest.xml']
    report.update({'source': str(options.source), 'unsignedOutput': str(options.out),
                   'sourceSha256': sha(options.source.read_bytes()), 'outputSha256': sha(options.out.read_bytes()),
                   'keptEntries': len(kept), 'changedPayloads': changed, 'removedV1SignatureEntries': removed,
                   'signed': False, 'installed': False, 'runtimeDebuggingVerified': False,
                   'signingInstruction': 'Root must sign with the same existing replica test key before any authorized in-place diagnostic update.'})
    (options.out.parent / 'debuggable-patch-report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding='utf-8')
    (options.out.parent / 'debuggable-entry-hashes.json').write_text(json.dumps(kept, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps({k: report[k] for k in ('addedAttribute', 'allExistingAttributeSemanticsUnchanged', 'unsignedOutput', 'changedPayloads')}, ensure_ascii=True))


if __name__ == '__main__':
    main()
