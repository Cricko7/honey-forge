"""Organization-scoped preflight and permission links for separate HTTP traps."""
from fastapi import HTTPException
from . import real_api


def validate_destinations(config, source_id):
    targets={}
    for block in config['blocks']:
        if block['kind']!='send':
            continue
        c=block['config']
        if c['target_id']==source_id:
            raise HTTPException(422,'Выберите отдельную ловушку-получатель')
        if c['target_id'] not in targets:
            trap=real_api._request('GET','/api/traps/'+c['target_id'])
            if trap['type_id']!='custom-http':
                raise HTTPException(422,'Получатель должен быть кастомной HTTP-ловушкой')
            targets[c['target_id']]=real_api._request('GET','/api/profiles/'+trap['profile_id'])['config']
        target=targets[c['target_id']]
        service=next((s for s in target['services'] if s['name']==c['service']),None)
        entry=next((b for b in target['blocks'] if service and b['id']==service['entry'] and b['kind']=='input'),None)
        if not entry or entry['config']['method'] not in ('ANY',c['method']) or entry['config']['path'] not in ('*',c['path']):
            raise HTTPException(422,'Эндпоинт не совпадает с HTTP-входом получателя')


def connect_destinations(config, source_id):
    for target_id in {b['config']['target_id'] for b in config['blocks'] if b['kind']=='send'}:
        try:
            real_api._request('POST','/api/trap-links',{'source_id':source_id,'target_id':target_id})
        except real_api.RealApiError as error:
            if error.code!='trap_link_exists':
                raise
