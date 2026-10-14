"""JSON pipeline delivery over management pipes; never opens outbound sockets."""
import asyncio
import base64
import datetime
import json
import uuid


class PipelineServerMixin:
    async def forward_pipeline(self, node_id, config, payload, data):
        key=str(uuid.uuid4())
        future=asyncio.get_running_loop().create_future()
        self.pipeline_pending[key]=future
        request={'request_id':key,'node_id':node_id,'target_id':config['target_id'],
                 'service':config['service'],'method':config['method'],'path':config['path'],
                 'payload':payload,'token':data.get('_pipeline_token',''),
                 'source_ip':data['_source_ip'],'source_port':data['_source_port']}
        try:
            send({'type':'pipeline_send','request':request})
            async with asyncio.timeout(7):
                if not await future:
                    raise FlowError('pipeline handoff rejected: check link, endpoint, agent and limits')
        except TimeoutError as error:
            raise FlowError('pipeline handoff timed out') from error
        finally:
            self.pipeline_pending.pop(key,None)

    async def receive_pipeline(self, d):
        service=next((s for s in self.config['services'] if s['name']==d['service']),None)
        if not service:
            return
        entry=self.blocks[service['entry']]['config']
        sequence=0
        session=str(uuid.uuid4())
        started=asyncio.get_running_loop().time()
        async def emit(kind,data):
            nonlocal sequence
            sequence+=1
            await self.emit({'event_id':str(uuid.uuid4()),'event_type':kind,
                'type_id':'custom-http','type_version':1,'profile_revision':self.snapshot['profile_revision'],
                'occurred_at':datetime.datetime.now(datetime.timezone.utc).isoformat().replace('+00:00','Z'),
                'session_id':session,'session_sequence':sequence,
                'source':{'ip':d['source_ip'],'port':d['source_port']},
                'destination':{'protocol':'tcp','port':service['port']},
                'data':{'service':service['name'],'pipeline_id':d['trace_id'],'pipeline_step':d['hop'],**data}})
        body=json.dumps(d['payload'],ensure_ascii=False,allow_nan=False,separators=(',',':'))
        reason='peer_closed'

        try:
            await emit('service.connection_opened',{})
            await emit('http.pipeline_received',{'request_id':d['request_id'],'source_id':d['source_id'],'node_id':service['entry']})
            if entry['method'] not in ('ANY',d['method']) or entry['path'] not in ('*',d['path']):
                raise FlowError('pipeline endpoint no longer matches')
            if len(body.encode())>self.max_body:
                raise FlowError('pipeline body exceeds target limit')
            await emit('http.request',{'method':d['method'],'path':d['path'],
                'payload_base64':base64.b64encode(body.encode()[:4096]).decode(),
                'captured_bytes':min(len(body.encode()),4096),'original_bytes':len(body.encode()),'truncated':len(body.encode())>4096})
            request={'json':d['payload'],'method':d['method'],'path':d['path'],'body':body,
                     'headers':{'content-type':'application/json'},'session':session,'request_id':session,
                     '_pipeline_token':d['token'],'_source_ip':d['source_ip'],'_source_port':d['source_port'],
                     'pipeline':{'id':d['trace_id'],'step':d['hop']}}
            expires=datetime.datetime.fromisoformat(d['expires_at'].replace('Z','+00:00'))
            remaining=(expires-datetime.datetime.now(datetime.timezone.utc)).total_seconds()
            if remaining<=0:
                raise FlowError('pipeline expired')
            async with asyncio.timeout(min(remaining,self.engine.timeout)):
                response=await self.engine.execute(service['entry'],request,emit)
            encoded=response['body'].encode() if response['status'] not in (204,304) else b''
            await emit('http.response',{'status':response['status'],'bytes_sent':len(encoded),
                'payload_base64':base64.b64encode(encoded[:4096]).decode(),'truncated':len(encoded)>4096})
        except (ValueError,KeyError,TypeError,TimeoutError) as error:
            reason='network_error'
            await emit('http.failure',{'message':'pipeline timeout' if isinstance(error,TimeoutError) else str(error)[:256]})
        except (RuntimeError,OSError):
            self.stop.set()
        finally:
            try:
                await emit('service.connection_closed',{'duration_ms':max(0,int((asyncio.get_running_loop().time()-started)*1000)),
                    'bytes_received':len(body.encode()),'reason':reason})
            except (RuntimeError,OSError,TimeoutError):
                self.stop.set()
