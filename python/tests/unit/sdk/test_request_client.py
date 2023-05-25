import unittest
from unittest.mock import patch, Mock

from aistore.sdk.const import (
    JSON_CONTENT_TYPE,
    HEADER_USER_AGENT,
    USER_AGENT_BASE,
    HEADER_CONTENT_TYPE,
)
from aistore.sdk.request_client import RequestClient
from aistore.version import __version__ as sdk_version


class TestRequestClient(unittest.TestCase):  # pylint: disable=unused-variable
    def setUp(self) -> None:
        self.endpoint = "https://aistore-endpoint"
        self.mock_session = Mock()
        with patch("aistore.sdk.request_client.requests") as mock_requests_lib:
            mock_requests_lib.session.return_value = self.mock_session
            self.request_client = RequestClient(self.endpoint)

        self.request_headers = {
            HEADER_CONTENT_TYPE: JSON_CONTENT_TYPE,
            HEADER_USER_AGENT: f"{USER_AGENT_BASE}/{sdk_version}",
        }

    def test_properties(self):
        self.assertEqual(self.endpoint + "/v1", self.request_client.base_url)
        self.assertEqual(self.endpoint, self.request_client.endpoint)
        self.assertEqual(self.mock_session, self.request_client.session)

    @patch("aistore.sdk.request_client.parse_raw_as")
    def test_request_deserialize(self, mock_parse):
        method = "method"
        path = "path"
        req_url = self.request_client.base_url + "/" + path

        deserialized_response = Mock()
        mock_response = Mock()
        mock_response.status_code = 200
        mock_response.text = "response_text"
        self.mock_session.request.return_value = mock_response
        mock_parse.return_value = deserialized_response
        res = self.request_client.request_deserialize(
            "method", "path", str, keyword="arg"
        )
        self.mock_session.request.assert_called_with(
            method, req_url, headers=self.request_headers, keyword="arg"
        )
        self.assertEqual(deserialized_response, res)

    def test_request(self):
        method = "method"
        path = "path"
        req_url = f"{self.request_client.base_url}/{path}"

        mock_response = Mock()
        mock_response.status_code = 200
        self.mock_session.request.return_value = mock_response
        res = self.request_client.request("method", "path", keyword="arg")
        self.mock_session.request.assert_called_with(
            method, req_url, headers=self.request_headers, keyword="arg"
        )
        self.assertEqual(mock_response, res)

        for response_code in [199, 300]:
            with patch("aistore.sdk.request_client.handle_errors") as mock_handle_err:
                mock_response.status_code = response_code
                self.mock_session.request.return_value = mock_response
                res = self.request_client.request("method", "path", keyword="arg")
                self.mock_session.request.assert_called_with(
                    method, req_url, headers=self.request_headers, keyword="arg"
                )
                self.assertEqual(mock_response, res)
                mock_handle_err.assert_called_once()

    def test_get_full_url(self):
        path = "/testpath/to_obj"
        params = {"p1key": "p1val", "p2key": "p2val"}
        res = self.request_client.get_full_url(path, params)
        self.assertEqual(
            "https://aistore-endpoint/v1/testpath/to_obj?p1key=p1val&p2key=p2val", res
        )
