import unittest
from unittest.mock import patch
from fastapi import HTTPException
from . import real_api
from .http_pipeline import validate_destinations, connect_destinations


class HTTPPipelineTests(unittest.TestCase):
    def config(self):
        return {'blocks':[{'kind':'send','config':{'target_id':'target','service':'web','method':'POST','path':'/pipeline'}}]}

    def test_destination_is_checked_before_deploy(self):
        for name,kind,method,path,valid in [('valid','custom-http','POST','/pipeline',True),('wrong type','custom-tcp','POST','/pipeline',False),('wrong method','custom-http','GET','/pipeline',False),('wrong endpoint','custom-http','POST','/other',False)]:
            with self.subTest(name=name):
                profile={'config':{'services':[{'name':'web','entry':'web'}],'blocks':[{'id':'web','kind':'input','config':{'method':method,'path':path}}]}}
                with patch.object(real_api,'_request',side_effect=[{'type_id':kind,'profile_id':'profile'},profile]):
                    if valid:validate_destinations(self.config(),None)
                    else:
                        with self.assertRaises(HTTPException):validate_destinations(self.config(),None)

    def test_self_link_and_foreign_target_are_rejected(self):
        with self.assertRaises(HTTPException):validate_destinations(self.config(),'target')
        with patch.object(real_api,'_request',side_effect=real_api.RealApiError(404,'resource_not_found','absent')):
            with self.assertRaises(real_api.RealApiError):validate_destinations(self.config(),None)

    def test_deploy_connects_allowed_link_and_only_ignores_duplicate(self):
        with patch.object(real_api,'_request',side_effect=real_api.RealApiError(409,'trap_link_exists','exists')) as request:
            connect_destinations(self.config(),'source')
            request.assert_called_once_with('POST','/api/trap-links',{'source_id':'source','target_id':'target'})
        with patch.object(real_api,'_request',side_effect=real_api.RealApiError(403,'forbidden','denied')):
            with self.assertRaises(real_api.RealApiError):connect_destinations(self.config(),'source')
